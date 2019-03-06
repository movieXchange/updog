package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	yaml "gopkg.in/yaml.v2"
)

var config = conf{}

var addr = flag.String("listen-address", ":8080", "The address to listen on for HTTP requests.")

var (
	httpDurationsHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "http_request_duration_seconds",
		Help: "HTTP Latency histogram",
	})

	healthCheckDependencyDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "updog_health_check_dependency_duration_seconds",
			Help: "Duration of a health check dependency in seconds",
		},
		[]string{"depenency"},
	)

	healthChecksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "updog_dependency_health_checks_total",
			Help: "Count of total health checks per dependency",
		},
		[]string{"dependency"},
	)

	healthChecksFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "updog_dependency_health_check_failures_total",
			Help: "Count of total health check failures per dependency",
		},
		[]string{"dependency"},
	)
)

func init() {

	config.getConf()

	fmt.Println("Dependencies:")
	for _, dep := range config.Dependencies {
		println(dep.Name, ":", dep.HTTPEndpoint)
	}

	prometheus.MustRegister(httpDurationsHistogram)
	prometheus.MustRegister(healthCheckDependencyDuration)
	prometheus.MustRegister(healthChecksTotal)
	prometheus.MustRegister(healthChecksFailuresTotal)
}

func main() {
	flag.Parse()

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/ping", handlePing)
	http.HandleFunc("/health", handleHealth)

	log.Fatal(http.ListenAndServe(*addr, nil))
}

func handlePing(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "pong")
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get("https://monitor.home.bencl.app/alertmanager/-/healthy")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("HTTP Response Status:", resp.Status)

	//declare status and duration channels. Perhaps a channel as a struct?
	ch := make(chan result)

	for _, dep := range config.Dependencies {
		go checkHealth(dep.HTTPEndpoint, dep.Name, ch)
	}

	for range config.Dependencies {
		<-ch
	}
}

func checkHealth(e, n string, ch chan<- result) {
	start := time.Now()
	fmt.Println("Starting dep check for", n, "at", start.UnixNano())

	//hit endpoint
	resp, err := http.Get(e)
	if err != nil {
		log.Fatal(err)
	}

	//stop timing
	elapsed := float64(time.Since(start).Seconds())
	fmt.Println("Ending dep check for", n, "after", elapsed)

	healthCheckDependencyDuration.WithLabelValues(n).Observe(elapsed)
	healthChecksTotal.WithLabelValues(n).Inc()

	//check response code
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		ch <- result{success: true, duration: elapsed}
	} else {
		healthChecksFailuresTotal.WithLabelValues(n).Inc()
		ch <- result{success: false, duration: elapsed}
	}
}

type result struct {
	success  bool
	duration float64
}

type conf struct {
	Dependencies []struct {
		HTTPEndpoint string `yaml:"http_endpoint"`
		Name         string `yaml:"name"`
		Type         string `yaml:"type"`
	} `yaml:"dependencies"`
}

func (c *conf) getConf() *conf {

	yamlFile, err := ioutil.ReadFile("sample-configuration.yaml")
	if err != nil {
		log.Printf("yamlFile.Get err   #%v ", err)
	}
	err = yaml.Unmarshal(yamlFile, c)
	if err != nil {
		log.Fatalf("Unmarshal: %v", err)
	}

	return c
}
