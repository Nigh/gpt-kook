package main

import (
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lonelyevil/kook"
	"github.com/lonelyevil/kook/log_adapter/plog"
	"github.com/phuslu/log"
	"github.com/spf13/viper"

	openaiezgo "github.com/Nigh/openai-ezgo"
	openai "github.com/sashabaranov/go-openai"
)

var botID string
var baseurl string
var tokenLimiter int

var localSession *kook.Session

var busyChannel map[string]bool

type ChannelConfig struct {
	ID             string `json:"id"`
	One2One        bool   `json:"one2one"`        // 一对一服务模式(必须At机器人以启动对话)
	MaxServiceTime int    `json:"maxServiceTime"` // 最大服务时间-秒
	MaxToken       int    `json:"maxToken"`       // 单次回复最大token

	enabled            bool
	currentServiceTime int    // 当前服务时间-秒
	currentServiceUser string // 当前服务用户
}

var channelSettings []ChannelConfig

var gptToken string
var botToken string

func init() {
	busyChannel = make(map[string]bool)
	channelSettings = make([]ChannelConfig, 0)

	viper.SetDefault("gpttokenmax", 512)
	viper.SetDefault("gpttoken", "0")
	viper.SetDefault("token", "0")
	viper.SetDefault("baseurl", openai.DefaultConfig("").BaseURL)
	viper.SetDefault("channels", []ChannelConfig{})
	viper.SetConfigType("json")
	viper.SetConfigName("config")
	viper.AddConfigPath("../config")
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("fatal error config file: %s", err))
	}

	botToken = viper.Get("token").(string)
	fmt.Println("botToken=" + botToken)
	gptToken = viper.Get("gpttoken").(string)
	fmt.Println("gptToken=" + gptToken)
	tokenLimiter = viper.GetInt("gpttokenmax")
	fmt.Println("gpttokenmax=" + strconv.Itoa(tokenLimiter))
	baseurl = viper.Get("baseurl").(string)
	fmt.Println("baseurl=" + baseurl)

	if err := viper.UnmarshalKey("channels", &channelSettings); err != nil {
		panic(err)
	}

	for idx := range channelSettings {
		busyChannel[channelSettings[idx].ID] = false
		channelSettings[idx].enabled = true
		if channelSettings[idx].MaxToken == 0 {
			channelSettings[idx].MaxToken = tokenLimiter
		}
	}
	for idx, v := range channelSettings {
		fmt.Printf("------ channel[%d] ------\n", idx)
		fmt.Println("\tID=" + v.ID)
		fmt.Println("\tOne2One=" + strconv.FormatBool(v.One2One))
		fmt.Println("\tMaxServiceTime=" + strconv.Itoa(v.MaxServiceTime))
		fmt.Println("\tMaxToken=" + strconv.Itoa(v.MaxToken))
	}
	go serviceTimeoutTimer()
}

func serviceTimeoutTimer() {
	timer := time.NewTicker(1 * time.Second)
	for range timer.C {
		for k, v := range channelSettings {
			if v.One2One && v.currentServiceUser != "" {
				channelSettings[k].currentServiceTime += 1
				// 超时结束对话
				if !busyChannel[v.ID] {
					if channelSettings[k].currentServiceTime > channelSettings[k].MaxServiceTime {
						channelSettings[k].currentServiceUser = ""
						channelSettings[k].currentServiceTime = 0
						_, err := sendMarkdown(v.ID, openaiezgo.EndSpeech(v.ID))
						if err != nil {
							fmt.Println("[ERROR]while trying to send Markdown")
						}
					}
				}
			}
		}
	}
}

func sendMarkdown(target string, content string) (resp *kook.MessageResp, err error) {
	resp, err = localSession.MessageCreate((&kook.MessageCreate{
		MessageCreateBase: kook.MessageCreateBase{
			Type:     kook.MessageTypeKMarkdown,
			TargetID: target,
			Content:  content,
		},
	}))
	if err != nil {
		fmt.Println("[ERROR]while trying to send Markdown:", content)
	}
	return
}

func sendMarkdownDirect(target string, content string) (mr *kook.MessageResp, err error) {
	return localSession.DirectMessageCreate(&kook.DirectMessageCreate{
		MessageCreateBase: kook.MessageCreateBase{
			Type:     kook.MessageTypeKMarkdown,
			TargetID: target,
			Content:  content,
		},
	})
}

func main() {
	l := log.Logger{
		Level:  log.InfoLevel,
		Writer: &log.ConsoleWriter{},
	}
	s := kook.New(botToken, plog.NewLogger(&l))
	me, _ := s.UserMe()
	fmt.Println("ID=" + me.ID)
	botID = me.ID
	s.AddHandler(markdownMessageHandler)
	s.Open()
	localSession = s

	cfg := openaiezgo.DefaultConfig(gptToken)
	cfg.BaseURL = baseurl
	cfg.MaxTokens = tokenLimiter
	cfg.TimeoutCallback = func(from string, token int) {
		sendMarkdown(from, "连续对话已超时结束。共消耗token:`"+strconv.Itoa(token)+"`")
		for idx := range channelSettings {
			if channelSettings[idx].ID == from {
				channelSettings[idx].currentServiceUser = ""
				channelSettings[idx].currentServiceTime = 0
				return
			}
		}
	}
	openaiezgo.NewClientWithConfig(cfg)

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running.")

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	fmt.Println("Bot will shutdown after 1 second.")

	<-time.After(time.Second * time.Duration(1))
	// Cleanly close down the KHL session.
	s.Close()
}

func markdownMessageHandler(ctx *kook.KmarkdownMessageContext) {
	if ctx.Extra.Author.Bot {
		return
	}
	if ctx.Common.TargetID == botID {
		go directMessageHandler(ctx)
	} else {
		go commonChanHandler(ctx)
	}
}

func directMessageHandler(ctx *kook.KmarkdownMessageContext) {
	var ctxCommon *kook.EventDataGeneral = ctx.Common

	reply := func(words string) string {
		resp, err := sendMarkdownDirect(ctxCommon.AuthorID, words)
		if err != nil {
			fmt.Println("[ERROR]while trying to send Markdown:", words)
			return ""
		}
		return resp.MsgID
	}
	reply("（小声）对不起，我们工作时间不允许私聊的哦。")
}

func instruction(one2one bool) (inst string) {
	inst = ""
	if one2one {
		inst += "At我即可开启一对一对话服务。\n"
	}
	inst += "命令说明：\n1. 发送 `调教 + 人格设定` 可以为我设置接下来聊天的人格。\n2. 发送 `结束对话` 可以立即终止对话服务。\n3. 发送 `帮助` 可以查看帮助信息。"
	return
}

func commonChanHandler(ctx *kook.KmarkdownMessageContext) {
	var ctxCommon *kook.EventDataGeneral = ctx.Common
	var ctxExtra kook.EventCustomMessage = ctx.Extra

	var validChannel int = -1
	for idx, v := range channelSettings {
		if ctxCommon.TargetID == v.ID {
			validChannel = idx
			break
		}
	}
	if validChannel < 0 {
		return
	}
	// 无效频道
	if !channelSettings[validChannel].enabled {
		return
	}

	reply := func(words string) string {
		resp, err := sendMarkdown(ctxCommon.TargetID, words)
		if err != nil {
			fmt.Println("[ERROR]while trying to send Markdown:", words)
			return ""
		}
		return resp.MsgID
	}
	words := strings.TrimSpace(ctxCommon.Content)
	if len(words) == 0 {
		return
	}

	regexpChat := func(words string) (ret int) {
		ret = 1
		help := regexp.MustCompile(`帮助.*`)
		if help.MatchString(words) {
			if channelSettings[validChannel].One2One {
				reply(instruction(true))
			} else {
				reply(instruction(false))
			}
			return
		}
		end := regexp.MustCompile(`结束对话.*`)
		if end.MatchString(words) {
			reply(openaiezgo.EndSpeech(ctxCommon.TargetID))
			return
		}
		cmd := regexp.MustCompile(`调教\s*(.*)`)
		if cmd.MatchString(words) {
			reply(openaiezgo.NewCharacterSet(ctxCommon.TargetID, cmd.FindStringSubmatch(words)[1]))
			return
		}
		ret = 0
		return
	}
	aiChat := func(words string) {
		if busyChannel[ctxCommon.TargetID] {
			reply("正在思考中。。。请稍等。。。")
			return
		}
		busyChannel[ctxCommon.TargetID] = true
		defer func() {
			delete(busyChannel, ctxCommon.TargetID)
		}()
		ctx.Session.MessageAddReaction(ctxCommon.MsgID, "🔸️")
		ans := openaiezgo.NewSpeech(ctxCommon.TargetID, words)
		if len(ans) > 0 {
			reply(ans)
		}
		ctx.Session.MessageDeleteReaction(ctxCommon.MsgID, "🔸️", botID)
		ctx.Session.MessageAddReaction(ctxCommon.MsgID, "✅")
	}

	if channelSettings[validChannel].One2One {
		// 一对一服务模式
		if channelSettings[validChannel].currentServiceUser != "" {
			// 服务中
			if ctxCommon.AuthorID != channelSettings[validChannel].currentServiceUser {
				return
			} else {
				// 通用处理
				if regexpChat(words) > 0 {
					return
				}
				// 响应对话
				aiChat(words)
			}
		} else {
			// 空闲中 at启动
			for _, id := range ctxExtra.Mention {
				if id == botID {
					channelSettings[validChannel].currentServiceUser = ctxCommon.AuthorID
					channelSettings[validChannel].currentServiceTime = 0
					reply("接受到用户 `" + ctxExtra.Author.Username + "` 的对话请求，进入一对一服务模式。从现在开始直到服务结束前，我都只会听你一个人的话。\n\n" + instruction(false))
					return
				}
			}
			// 通用处理
			if regexpChat(words) > 0 {
				return
			}
		}
	} else {
		// 通用处理
		if regexpChat(words) > 0 {
			return
		}
		// 响应对话
		aiChat(words)
	}
}
