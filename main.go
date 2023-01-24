package main

import (
	"flag"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/vvampirius/mygolibs/telegram"
	"log"
	"net/http"
	"os"
	"path"
)

const VERSION = `0.2`

var (
	ErrorLog         = log.New(os.Stderr, `error#`, log.Lshortfile)
	DebugLog         = log.New(os.Stdout, `debug#`, log.Lshortfile)
	PrometheusErrors = prometheus.NewCounterVec(prometheus.CounterOpts{Name: `errors`,
		Help: `Errors counter`}, []string{`action`})
	PrometheusNewItems  = prometheus.NewCounter(prometheus.CounterOpts{Name: `new_items`, Help: `Received new items`})
	PrometheusSendItems = prometheus.NewCounterVec(prometheus.CounterOpts{Name: `send_items`, Help: `Items to send`},
		[]string{`username`})
)

func helpText() {
	fmt.Println(`# https://github.com/vvampirius/onliner-auto-bot`)
	flag.PrintDefaults()
}

func Pong(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprint(w, `PONG`)
}

func main() {
	help := flag.Bool("h", false, "print this help")
	ver := flag.Bool("v", false, "Show version")
	configFilePath := flag.String("c", "config.yml", "Path to YAML config")
	flag.Parse()

	if *help {
		helpText()
		os.Exit(0)
	}

	if *ver {
		fmt.Println(VERSION)
		os.Exit(0)
	}

	fmt.Printf("Starting version %s...\n", VERSION)

	if err := prometheus.Register(PrometheusErrors); err != nil {
		ErrorLog.Println(err.Error())
		os.Exit(1)
	}
	if err := prometheus.Register(PrometheusNewItems); err != nil {
		ErrorLog.Println(err.Error())
		os.Exit(1)
	}
	if err := prometheus.Register(PrometheusSendItems); err != nil {
		ErrorLog.Println(err.Error())
		os.Exit(1)
	}

	configFile, err := NewConfigFile(*configFilePath)
	if err != nil {
		os.Exit(1)
	}

	me, err := telegram.GetMe(configFile.Config.Telegram.Token)
	if err != nil {
		os.Exit(1)
	}
	DebugLog.Printf("Got info from Telegram API: @%s with ID:%d and name '%s'\n", me.Username, me.Id, me.FirstName)

	if err := telegram.SetWebHook(configFile.Config.Telegram.Token, configFile.Config.Telegram.Webhook); err != nil {
		ErrorLog.Println(err.Error())
		os.Exit(1)
	}
	DebugLog.Printf("Callback URL set to '%s'\n", configFile.Config.Telegram.Webhook)

	telegramApi := telegram.NewApi(configFile.Config.Telegram.Token)

	state, err := NewState(path.Join(configFile.Config.BaseDir, `state.yml`))
	if err != nil {
		os.Exit(1)
	}

	core, err := NewCore(configFile, telegramApi, state)
	if err != nil {
		os.Exit(1)
	}

	server := http.Server{Addr: configFile.Config.Listen}
	http.HandleFunc(`/ping`, Pong)
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc(`/rss`, core.RssHttpHandler)
	http.HandleFunc(`/`, core.TelegramHttpHandler)
	if err := server.ListenAndServe(); err != nil {
		ErrorLog.Fatalln(err.Error())
	}
}
