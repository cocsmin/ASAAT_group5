// Command devbroker is a small embedded MQTT broker for running the factory
// without Docker (go run ./cmd/dev starts it). Docker uses Mosquitto instead.
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

func main() {
	addr := ":1883"
	if v := os.Getenv("BROKER_ADDR"); v != "" {
		addr = v
	}
	s := mqtt.New(&mqtt.Options{InlineClient: true})
	_ = s.AddHook(new(auth.AllowHook), nil)
	if err := s.AddListener(listeners.NewTCP(listeners.Config{ID: "tcp", Address: addr})); err != nil {
		log.Fatal(err)
	}
	go func() {
		if err := s.Serve(); err != nil {
			log.Fatal(err)
		}
	}()
	log.Printf("dev MQTT broker listening on %s", addr)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	_ = s.Close()
}
