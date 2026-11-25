// Command loadtest measures the MQTT broker under the message load of a bigger plant (docs/scalability.md).
// It starts N simulated nodes in the Sparkplug group "LoadTest" (the factory itself uses "CakeFactory", so
// the real stations and the monitor are not disturbed). Every node holds its own connection with the same
// keep-alive as the stations and sends a heartbeat (QoS 0, one every T_hb); a few publishers add a stream of
// reliable handoff messages (QoS 1). One subscriber receives everything and measures the delay from publish
// to receive (same process, same clock). One CSV row per step.
//
//	go run ./cmd/loadtest -broker tcp://127.0.0.1:1883 -nodes 16,160,800,1600 -step 40s -out docs/loadtest-results.csv
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"cakefactory/internal/sparkplug"
)

const group = "LoadTest"

func main() {
	broker := flag.String("broker", "tcp://127.0.0.1:1883", "MQTT broker")
	nodesFlag := flag.String("nodes", "16,160,800,1600", "numbers of simulated nodes, one step each")
	step := flag.Duration("step", 40*time.Second, "length of one step")
	hb := flag.Duration("hb", 5*time.Second, "heartbeat interval T_hb of every node")
	handoffs := flag.Int("handoffs", 50, "QoS 1 handoff messages per second in addition to the heartbeats")
	keepAlive := flag.Duration("keepalive", 5*time.Second, "MQTT keep-alive of every node")
	out := flag.String("out", "", "CSV file to write")
	flag.Parse()

	var rows [][]string
	fmt.Printf("%7s %9s %9s %9s %8s %9s %9s %9s %9s\n", "nodes", "offered/s", "received", "lost %", "conn s", "p50 ms", "p95 ms", "p99 ms", "max ms")
	for _, ns := range strings.Split(*nodesFlag, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(ns))
		if err != nil || n < 1 {
			fmt.Fprintln(os.Stderr, "bad -nodes:", ns)
			os.Exit(2)
		}
		r := run(*broker, n, *step, *hb, *handoffs, *keepAlive)
		fmt.Println(r.line())
		rows = append(rows, r.csv())
		time.Sleep(3 * time.Second) // let the broker settle between steps
	}
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		fmt.Fprintln(f, "timestamp,nodes,hb_s,handoffs_per_s,offered_per_s,sent,received,lost_pct,connect_s,p50_ms,p95_ms,p99_ms,max_ms")
		for _, r := range rows {
			fmt.Fprintln(f, strings.Join(r, ","))
		}
	}
}

type result struct {
	nodes              int
	hb                 time.Duration
	handoffs           int
	sent, received     int64
	offered            float64
	connect            time.Duration
	p50, p95, p99, max float64
	at                 time.Time
	lostPct            float64
}

func (r result) line() string {
	return fmt.Sprintf("%7d %9.1f %9d %9.3f %8.1f %9.2f %9.2f %9.2f %9.2f", r.nodes, r.offered, r.received, r.lostPct, r.connect.Seconds(), r.p50, r.p95, r.p99, r.max)
}

func (r result) csv() []string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }
	return []string{r.at.UTC().Format(time.RFC3339), strconv.Itoa(r.nodes), f(r.hb.Seconds()), strconv.Itoa(r.handoffs), f(r.offered),
		strconv.FormatInt(r.sent, 10), strconv.FormatInt(r.received, 10), f(r.lostPct), f(r.connect.Seconds()), f(r.p50), f(r.p95), f(r.p99), f(r.max)}
}

func client(broker, id string, keepAlive time.Duration, clean bool) mqtt.Client {
	o := mqtt.NewClientOptions().AddBroker(broker).SetClientID(id).SetCleanSession(clean).SetKeepAlive(keepAlive).
		SetPingTimeout(keepAlive).SetAutoReconnect(true).SetConnectTimeout(20 * time.Second)
	c := mqtt.NewClient(o)
	if t := c.Connect(); !t.WaitTimeout(30*time.Second) || t.Error() != nil {
		fmt.Fprintln(os.Stderr, "connect failed:", id, t.Error())
		os.Exit(1)
	}
	return c
}

func payload() []byte {
	// a heartbeat of a station: uptime, two device states, three gauges (about 56 bytes), plus the send time
	p := sparkplug.Payload{Timestamp: uint64(time.Now().UnixMilli()), Metrics: []sparkplug.Metric{
		sparkplug.NewMetric("sentNs", time.Now().UnixNano()),
		sparkplug.NewMetric("Machine status (heartbeat)/uptime", int64(1234)),
		sparkplug.NewMetric("Machine status (heartbeat)/dev.Depositor", "ok"),
		sparkplug.NewMetric("Machine status (heartbeat)/g.queue", int64(2)),
	}}
	return p.Marshal()
}

func run(broker string, n int, step, hb time.Duration, handoffs int, keepAlive time.Duration) result {
	var recv atomic.Int64
	var mu sync.Mutex
	var lat []float64
	sub := client(broker, fmt.Sprintf("loadtest-sub-%d", time.Now().UnixNano()), keepAlive, true)
	sub.Subscribe(sparkplug.Namespace+"/"+group+"/#", 1, func(_ mqtt.Client, m mqtt.Message) {
		p, err := sparkplug.Unmarshal(m.Payload())
		if err != nil {
			return
		}
		for _, mt := range p.Metrics {
			if mt.Name == "sentNs" {
				if v, ok := mt.Value.(int64); ok {
					d := float64(time.Now().UnixNano()-v) / 1e6
					mu.Lock()
					lat = append(lat, d)
					mu.Unlock()
				}
			}
		}
		recv.Add(1)
	}).Wait()

	// connect the nodes (a few at a time, like containers that start together)
	t0 := time.Now()
	clients := make([]mqtt.Client, n)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 50)
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			clients[i] = client(broker, fmt.Sprintf("loadtest-%d-%d", n, i), keepAlive, true)
			<-sem
		}(i)
	}
	wg.Wait()
	connect := time.Since(t0)

	var sent atomic.Int64
	stop := make(chan struct{})
	var pw sync.WaitGroup
	for i := 0; i < n; i++ {
		pw.Add(1)
		go func(i int) {
			defer pw.Done()
			topic := sparkplug.Topic(group, sparkplug.NData, fmt.Sprintf("NODE-%04d", i), "")
			time.Sleep(time.Duration(rand.Int63n(int64(hb)))) // nodes are not in step
			tk := time.NewTicker(hb)
			defer tk.Stop()
			for {
				clients[i].Publish(topic, 0, false, payload())
				sent.Add(1)
				select {
				case <-stop:
					return
				case <-tk.C:
				}
			}
		}(i)
	}
	// reliable handoff stream from a few publishers
	pubs := 5
	if handoffs > 0 {
		for k := 0; k < pubs; k++ {
			pw.Add(1)
			go func(k int) {
				defer pw.Done()
				topic := sparkplug.Topic(group, sparkplug.NData, fmt.Sprintf("HANDOFF-%d", k), "")
				tk := time.NewTicker(time.Second * time.Duration(pubs) / time.Duration(handoffs))
				defer tk.Stop()
				for {
					select {
					case <-stop:
						return
					case <-tk.C:
						clients[k%n].Publish(topic, 1, false, payload())
						sent.Add(1)
					}
				}
			}(k)
		}
	}
	time.Sleep(step)
	close(stop)
	pw.Wait()
	time.Sleep(2 * time.Second) // let the last messages arrive
	s, r := sent.Load(), recv.Load()
	for _, c := range clients {
		c.Disconnect(100)
	}
	sub.Disconnect(100)

	mu.Lock()
	defer mu.Unlock()
	sort.Float64s(lat)
	q := func(p float64) float64 {
		if len(lat) == 0 {
			return 0
		}
		return lat[int(p*float64(len(lat)-1))]
	}
	lost := 0.0
	if s > 0 {
		lost = float64(s-r) / float64(s) * 100
	}
	return result{nodes: n, hb: hb, handoffs: handoffs, sent: s, received: r, offered: float64(s) / step.Seconds(), connect: connect,
		p50: q(0.5), p95: q(0.95), p99: q(0.99), max: q(1), at: time.Now(), lostPct: lost}
}
