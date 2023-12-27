package main

import (
	"context"
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

	openai "github.com/sashabaranov/go-openai"
)

var aiChannel string
var botID string
var baseurl string

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
	baseurl = viper.Get("baseurl").(string)
	fmt.Println("baseurl=" + baseurl)

	s := kook.New(token, plog.NewLogger(&l))
	me, _ := s.UserMe()
	fmt.Println("ID=" + me.ID)
	botID = me.ID
	s.AddHandler(markdownMessageHandler)
	s.Open()
	localSession = s

	gptConfig := openai.DefaultConfig(gpttoken)
	gptConfig.BaseURL = baseurl
	aiClient = openai.NewClientWithConfig(gptConfig)

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running.")
	go continueChatTimer()

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

// 连续对话支持
var chatHistory []openai.ChatCompletionMessage

func talk2GPT(words string, role string) (string, int, int) {
	chatHistory = append(chatHistory, openai.ChatCompletionMessage{
		Role:    role,
		Content: words,
	})
	resp, err := aiClient.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model:    openai.GPT3Dot5Turbo,
			Messages: chatHistory,
		},
	)
	if err != nil {
		fmt.Printf("ChatCompletion error: %v\n", err)
		return "", 0, 0
	}
	chatHistory = append(chatHistory, resp.Choices[0].Message)
	fmt.Printf("GPT: %s\n", resp.Choices[0].Message.Content)
	return resp.Choices[0].Message.Content, resp.Usage.PromptTokens, resp.Usage.CompletionTokens
}

var chatContinueSignal chan struct{}

func chatContinue() {
	go func() {
		chatContinueSignal <- struct{}{}
	}()
}

func continueChatTimer() {
	var timeout int = 30
	chatContinueSignal = make(chan struct{}, 1)
	for {
		select {
		case <-chatContinueSignal:
			timeout = 30
			return
		case <-time.After(time.Duration(10 * time.Second)):
			if timeout > 0 {
				timeout -= 1
				if timeout == 0 {
					if len(chatHistory) > 0 {
						timeout = 30
						chatHistory = []openai.ChatCompletionMessage{}
						sendMarkdown(aiChannel, "连续对话已超时结束。继续聊天开启新的对话。")
					}
				}
			}
			return
		}
	}
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
	chatContinue()
	words := strings.TrimSpace(ctxCommon.Content)
	if len(words) > 0 {
		role := openai.ChatMessageRoleUser
		rst := regexp.MustCompile(`重置对话.*`)
		if rst.MatchString(words) {
			if len(chatHistory) > 0 {
				chatHistory = []openai.ChatCompletionMessage{}
				reply("对话已重置。继续聊天开启新的对话。")
			} else {
				reply("没有对话可以重置。请问有其他可以帮助您的吗？")
			}
			return
		}
		cmd := regexp.MustCompile(`调教\s*(.*)`)
		if cmd.MatchString(words) {
			words = cmd.ReplaceAllString(words, "$1")
			role = openai.ChatMessageRoleSystem
		}
		ans, tokenIn, tokenOut := talk2GPT(words, role)
		if len(ans) > 0 {
			reply(ans + "\n`token:" + strconv.Itoa(tokenIn) + "," + strconv.Itoa(tokenOut) + "`")
			for len(chatHistory) > 16 {
				chatHistory = chatHistory[1:]
			}
		}
	}
}
