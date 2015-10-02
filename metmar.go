package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

type Region struct {
	Situation   string `json:"situation"`
	Observation string `json:"observation"`
	WindAndSea  string `json:"ventEtMer"`
	Waves       string `json:"houle"`
	Visibility  string `json:"visi"`
	Confidence  string `json:"indice"`
}

type Echeance struct {
	Title string    `json:"titreEcheance"`
	Areas []*Region `json:"region"`
}

type Bulletin struct {
	Title     string      `json:"titreBulletin"`
	Special   string      `json:"bulletinSpecial"`
	Echeances []*Echeance `json:"echeance"`
}

func formatReport(b *Bulletin) string {
	buf := &bytes.Buffer{}
	w := func(format string, args ...interface{}) {
		fmt.Fprintf(buf, format, args...)
	}
	wif := func(s string) {
		if s != "" {
			s = strings.Replace(s, "<br />", "\n", -1)
			w("%s\n", s)
		}
	}
	w("%s\n", b.Title)
	w("Bulletin spécial: %s\n", b.Special)
	w("\n")
	for _, e := range b.Echeances {
		w("%s\n", e.Title)
		for _, r := range e.Areas {
			wif(r.Situation)
			wif(r.Observation)
			parts := strings.Split(r.WindAndSea, "MER :")
			if len(parts) == 2 {
				wif(parts[0])
				wif("MER :" + parts[1])
			} else {
				wif(r.WindAndSea)
			}
			wif(r.Waves)
			wif(r.Visibility)
			wif(r.Confidence)
		}
		w("\n")
	}
	return buf.String()
}

func hashReport(report string) string {
	h := sha256.Sum256([]byte(report))
	return hex.EncodeToString(h[:])
}

func fetchWeather() (string, error) {
	url := "http://www.meteofrance.com/mf3-rpc-portlet/rest/bulletins/cote/3/bulletinsMarineMetropole"
	rsp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("could not fetch weather: %s", err)
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != 200 {
		return "", fmt.Errorf("failed to fetch weather: %s", rsp.Status)
	}
	reports := []*Bulletin{}
	err = json.NewDecoder(rsp.Body).Decode(&reports)
	if err != nil {
		return "", fmt.Errorf("could not decode json response: %s", err)
	}
	if len(reports) <= 0 {
		return "", fmt.Errorf("no report retrieved")
	}
	return formatReport(reports[0]), nil
}

func formatJsonWeather(w http.ResponseWriter, req *http.Request) {
	report, err := fetchWeather()
	w.Header().Set("Content-Type", "text/plain;charset=utf-8")
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "error: %s\n", err)
		return
	}
	h := hashReport(report)
	w.Header().Set("ETag", h)
	etag := req.Header.Get("If-None-Match")
	if etag == h {
		w.WriteHeader(304)
		return
	}
	fmt.Fprintf(w, "%s", report)
}

func metmar(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("web server address expected")
	}
	addr := args[0]
	http.HandleFunc("/", formatJsonWeather)
	return http.ListenAndServe(addr, nil)
}

func main() {
	err := metmar(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
