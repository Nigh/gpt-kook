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

var aiChannel string
var botID string
var baseurl string
var tokenLimiter int

var localSession *kook.Session
var aiClient *openai.Client

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
	viper.SetDefault("gpttokenmax", 512)
	viper.SetDefault("gpttoken", "0")
	viper.SetDefault("token", "0")
	viper.SetDefault("aiChannel", "0")
	viper.SetDefault("baseurl", openai.DefaultConfig("").BaseURL)
	viper.SetConfigType("json")
	viper.SetConfigName("config")
	viper.AddConfigPath("/config")
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("fatal error config file: %s", err))
	}
	aiChannel = viper.Get("aiChannel").(string)

	l := log.Logger{
		Level:  log.InfoLevel,
		Writer: &log.ConsoleWriter{},
	}
	token := viper.Get("token").(string)
	fmt.Println("token=" + token)
	gpttoken := viper.Get("gpttoken").(string)
	fmt.Println("gpttoken=" + gpttoken)
	tokenLimiter = viper.GetInt("gpttokenmax")
	fmt.Println("gpttokenmax=" + strconv.Itoa(tokenLimiter))
	baseurl = viper.Get("baseurl").(string)
	fmt.Println("baseurl=" + baseurl)

	s := kook.New(token, plog.NewLogger(&l))
	me, _ := s.UserMe()
	fmt.Println("ID=" + me.ID)
	botID = me.ID
	s.AddHandler(markdownMessageHandler)
	s.Open()
	localSession = s

	cfg := openaiezgo.DefaultConfig(gpttoken)
	cfg.BaseURL = baseurl
	cfg.MaxTokens = tokenLimiter
	cfg.TimeoutCallback = func(from string, token int) {
		sendMarkdown(from, "连续对话已超时结束。共消耗token:`"+strconv.Itoa(token)+"`")
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
	switch ctx.Common.TargetID {
	case botID:
		directMessageHandler(ctx.Common)
	case aiChannel:
		commonChanHandler(ctx.Common)
	}
}

func directMessageHandler(ctxCommon *kook.EventDataGeneral) {
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

func commonChanHandler(ctxCommon *kook.EventDataGeneral) {
	reply := func(words string) string {
		resp, err := sendMarkdown(ctxCommon.TargetID, words)
		if err != nil {
			fmt.Println("[ERROR]while trying to send Markdown:", words)
			return ""
		}
		return resp.MsgID
	}
	words := strings.TrimSpace(ctxCommon.Content)
	if len(words) > 0 {
		rst := regexp.MustCompile(`重置对话.*`)
		if rst.MatchString(words) {
			reply(openaiezgo.EndSpeech(ctxCommon.TargetID))
			return
		}
		cmd := regexp.MustCompile(`调教\s*(.*)`)
		if cmd.MatchString(words) {
			reply(openaiezgo.NewCharacterSet(ctxCommon.TargetID, cmd.FindStringSubmatch(words)[1]))
			return
		}
		ans := openaiezgo.NewSpeech(ctxCommon.TargetID, words)
		if len(ans) > 0 {
			reply(ans)
		}
	}
}
