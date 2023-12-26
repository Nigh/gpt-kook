package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lonelyevil/kook"
	"github.com/lonelyevil/kook/log_adapter/plog"
	"github.com/phuslu/log"
	"github.com/spf13/viper"
)

var aiChannel string
var botID string

var localSession *kook.Session

func sendKCard(target string, content string) (resp *kook.MessageResp, err error) {
	resp, err = localSession.MessageCreate((&kook.MessageCreate{
		MessageCreateBase: kook.MessageCreateBase{
			Type:     kook.MessageTypeCard,
			TargetID: target,
			Content:  content,
		},
	}))
	if err != nil {
		fmt.Println("[ERROR]while trying to send KCard:", content)
	}
	return
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
	viper.SetDefault("token", "0")
	viper.SetDefault("aiChannel", "0")
	viper.SetConfigType("json")
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("Fatal error config file: %s \n", err))
	}
	aiChannel = viper.Get("aiChannel").(string)

	l := log.Logger{
		Level:  log.InfoLevel,
		Writer: &log.ConsoleWriter{},
	}
	token := viper.Get("token").(string)
	fmt.Println("token=" + token)

	s := kook.New(token, plog.NewLogger(&l))
	me, _ := s.UserMe()
	fmt.Println("ID=" + me.ID)
	botID = me.ID
	s.AddHandler(markdownMessageHandler)
	s.Open()
	localSession = s

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

func statuHan(ctx *kook.BotJoinContext) {
	fmt.Println("Bot Join:" + ctx.Extra.GuildID)
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
		resp, _ := sendMarkdownDirect(ctxCommon.AuthorID, words)
		return resp.MsgID
	}
	reply("（小声）对不起，我们工作时间不允许私聊的哦。")
}

func commonChanHandler(ctxCommon *kook.EventDataGeneral) {
	reply := func(words string) string {
		resp, _ := sendMarkdown(ctxCommon.TargetID, words)
		return resp.MsgID
	}

	reply("非常抱歉。YUI 当前正在维护，请等待 YUI 维护结束。")
}
