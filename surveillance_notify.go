package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/gregdel/pushover"
	"github.com/op/go-logging"
)

var (
	flags     SurveillanceFlags
	log       = logging.MustGetLogger("surveillance-client")
	format    = logging.MustStringFormatter(`%{color}%{time:15:04:05.000} %{shortfunc:12.12s} ▶ %{level:.5s} %{color:reset} %{message}`)
	userToken = ""
	appToken  = ""
)

type SurveillanceFlags struct {
	hostname string
	port     int
	topic    string
	verbose  bool
	logfile  string
}

func InitFlags() (flags SurveillanceFlags) {
	flag.StringVar(&flags.hostname, "h", "localhost", "MQTT Hostname to connect to, defaults to localhost")
	flag.IntVar(&flags.port, "p", 1883, "MQTT Port, defaults to 1883")
	flag.StringVar(&flags.topic, "t", "/surveillance/notification/#", "MQTT Topic to publish to")
	flag.BoolVar(&flags.verbose, "v", false, "Enable verbose logging")
	flag.StringVar(&flags.logfile, "l", "stderr", "Enable logging to file")
	flag.Parse()
	return flags
}

func Init() {
	var outfile []*os.File
	outfile = append(outfile, os.Stderr)

	if flags.logfile != "stderr" {
		f, err := os.OpenFile(flags.logfile, os.O_RDWR|os.O_CREATE|os.O_APPEND, os.ModePerm)
		if err != nil {
			log.Error(err)
		} else {
			outfile = append(outfile, f)
		}
	}

	var backends []logging.Backend

	for _, of := range outfile {
		backend1 := logging.NewLogBackend(of, "", 0)
		backend1Formatter := logging.NewBackendFormatter(backend1, format)
		backend1Leveled := logging.AddModuleLevel(backend1Formatter)
		if flags.verbose == false {
			backend1Leveled.SetLevel(logging.INFO, "")
		} else {
			log.Info("Enabling verbose logging")
			backend1Leveled.SetLevel(logging.DEBUG, "")
		}
		backends = append(backends, backend1Leveled)
	}
	logging.SetBackend(backends...)
}

func OnConnect(client MQTT.Client) {
	log.Info("Subscribing to ", flags.topic)
	if token := client.Subscribe(flags.topic, 1, nil); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}
}

var OnMessage MQTT.MessageHandler = func(client MQTT.Client, msg MQTT.Message) {
	log.Debug("TOPIC:", msg.Topic())
	log.Debug("MSG:", string(msg.Payload()))
	var message map[string]interface{}
	if err := json.Unmarshal(msg.Payload(), &message); err != nil {
		panic(err)
	}
	if k, ok := message["notification"]; ok {
		body := k.(string)
		app := pushover.New(appToken)
		recipient := pushover.NewRecipient(userToken)
		log.Debug("Sending notification:", body)
		message := pushover.NewMessage(body)
		_, err := app.SendMessage(message, recipient)
		if err != nil {
			log.Error(err)
		}
	} else {
		log.Debug("Error parsing message as notification :", message)
	}
}

func SetupMQTT(messages chan string) MQTT.Client {
	url := fmt.Sprintf("tcp://%s:%d", flags.hostname, flags.port)
	opts := MQTT.NewClientOptions().AddBroker(url)
	host, err := os.Hostname()
	clientId := "surveillance-notify-"
	if err != nil {
		log.Error(err)
	} else {
		clientId += host
	}
	opts.SetClientID(clientId)
	opts.SetDefaultPublishHandler(OnMessage)
	opts.SetAutoReconnect(true)
	opts.SetCleanSession(true)
	opts.SetOnConnectHandler(OnConnect)

	log.Info("Connecting to ", url)
	c := MQTT.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	log.Info("Done setting up MQTT")
	return c
}

func main() {
	messages := make(chan string)
	flags = InitFlags()
	log.Debug("Starting with parameters : ", flags)
	Init()
	SetupMQTT(messages)
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)

	go func() {
		for {
			<-c
			Init()
		}
	}()
	<-messages
}
