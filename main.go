package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const defaultInterval = "2s"

var (
	isMaster = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "is_master",
			Help: "Is master node(1) or backup node(0)",
		})
)

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	h := promhttp.Handler()
	h.ServeHTTP(w, r)
}

func main() {
	var intervalStr = getEnv("INTERVAL", defaultInterval)
	var interval, intervalErr = time.ParseDuration(intervalStr)
	if intervalErr != nil {
		fmt.Printf("Invalid INTERVAL duration : `%s`", intervalStr)
		log.Fatal(intervalErr)
	}

	var vipStr = getEnv("VIP", "")

	fmt.Printf("Using Config :\n")
	fmt.Printf("\tInterval : %v \n", interval)
	fmt.Printf("\tVIP      : %v \n", vipStr)
	fmt.Printf("\n\n")

	resetCounters()
	// Create Server and Route Handlers
	httpRouter := mux.NewRouter()

	//httpRouter.HandleFunc("/", handler)
	httpRouter.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
			<head><title>Keepalived-IPVS Checker</title></head>
			<body>
			<h1>Keepalived-IPVS Checker</h1>
			<p><a href="/metrics">Metrics</a></p>
			</body>
			</html>`))
	})
	httpRouter.HandleFunc("/metrics", metricsHandler)

	srv := &http.Server{
		Handler:      httpRouter,
		Addr:         ":8080",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Start Server
	go func() {
		log.Println("Starting WebServer on port 8080")
		if err := srv.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}()

	go func() {
		defer starTimer(interval, vipStr)
		updateMetrics(vipStr)
	}()

	// Graceful Shutdown
	waitForShutdown(srv)
}

func resetCounters() {
	isMaster.Set(0)
}
func starTimer(interval time.Duration, vipStr string) {

	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				updateMetrics(vipStr)
			}
		}
	}()
}

func updateMetrics(vipStr string) {
	if vipStr != "" {
		hasVip, err := checkVip(vipStr)
		if err != nil {
			log.Println(err)
		} else if hasVip {
			isMaster.Set(1)
		} else {
			isMaster.Set(0)
		}
	}
}

func checkVip(vipStr string) (bool, error) {
	ifaces, err := net.Interfaces()
	// handle err
	if err != nil {
		return false, err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue // interface down
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue // loopback interface
		}
		addrs, err := iface.Addrs()
		if err != nil {
			log.Println(err)
			return false, err
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue // not an ipv4 address
			}
			var ipStr = ip.String()
			if vipStr == ipStr {
				return true, nil
			}
		}
	}
	return false, nil
}

func waitForShutdown(srv *http.Server) {
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Block until we receive our signal.
	<-interruptChan

	// Create a deadline to wait for.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	srv.Shutdown(ctx)

	log.Println("Shutting down")
	os.Exit(0)
}

func getEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}
