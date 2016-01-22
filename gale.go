package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

type GaleWarning struct {
	Number int
	Date   time.Time
}

// Bulletin spécial: Avis de Grand frais à Coup de vent numéro 36
var (
	reWarning = regexp.MustCompile(`^\s*Bulletin spécial:.*(?:nr|numéro|n°)\s+(\d+)`)
)

// extractWarningNumber returns the gale warning number in supplied weatcher
// forecast. It returns zero if there is none.
func extractWarningNumber(path string) (int, error) {
	fp, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer fp.Close()

	scanner := bufio.NewScanner(fp)
	for scanner.Scan() {
		m := reWarning.FindSubmatch(scanner.Bytes())
		if m != nil {
			n, err := strconv.ParseInt(string(m[1]), 10, 32)
			if err != nil {
				return 0, err
			}
			return int(n), nil
		}
	}
	return 0, scanner.Err()
}

var (
	rePath = regexp.MustCompile(`^.*(\d{4}_\d{2}_\d{2}T\d{2}_\d{2}_\d{2})\.txt$`)
)

// extractWarningNumbers returns the sequence of gale warnings extracted from
// weather forecasts in supplied directory.
func extractWarningNumbers(dir string) ([]GaleWarning, error) {

	warnings := []GaleWarning{}
	err := filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || !fi.Mode().IsRegular() {
			return err
		}
		m := rePath.FindStringSubmatch(path)
		if m == nil {
			return nil
		}
		d, err := time.Parse("2006_01_02T15_04_05", m[1])
		if err != nil {
			return err
		}
		n, err := extractWarningNumber(path)
		if err != nil {
			return err
		}
		if n > 0 {
			warnings = append(warnings, GaleWarning{
				Number: n,
				Date:   d,
			})
		}
		return nil
	})
	return warnings, err
}

func serveGaleWarnings(galeDir string, template []byte, w http.ResponseWriter,
	req *http.Request) error {

	warnings, err := extractWarningNumbers(galeDir)
	if err != nil {
		return err
	}

	baseDate := time.Date(2016, time.January, 1, 0, 0, 0, 0, time.UTC)

	type warningOffset struct {
		X       float64 `json:"x"`
		Y       float64 `json:"y"`
		Date    string  `json:"date"`
		YearDay int     `json:"yearday"`
	}
	offsets := []warningOffset{}
	refs := []warningOffset{}
	for _, w := range warnings {
		deltaDays := w.Date.Sub(baseDate).Hours() / 24.
		offset := warningOffset{
			X:       deltaDays,
			Y:       float64(w.Number),
			Date:    w.Date.Format("2006-01-02 15:04:05"),
			YearDay: w.Date.YearDay(),
		}
		offsets = append(offsets, offset)
		offset.Y = float64(offset.YearDay)
		refs = append(refs, offset)
	}

	dataVar, err := json.Marshal(&offsets)
	if err != nil {
		return err
	}
	refVar, err := json.Marshal(&refs)
	if err != nil {
		return err
	}
	page := bytes.Replace(template, []byte("$DATA"), dataVar, -1)
	page = bytes.Replace(page, []byte("$REF"), refVar, -1)
	w.Header().Set("Content-Type", "text/html")
	_, err = w.Write(page)
	return err
}

func handleGaleWarnings(galeDir string, template []byte, w http.ResponseWriter,
	req *http.Request) {

	err := serveGaleWarnings(galeDir, template, w, req)
	if err != nil {
		log.Printf("error: %s\n", err)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(500)
		w.Write([]byte(fmt.Sprintf("error: %s", err)))
	}
}

var (
	galeCmd = app.Command("gale", "display gale warning number vs day in the year")
	galeDir = galeCmd.Arg("forecastdir", "directory container weather forecasts").
		Required().String()
	galePrefix = galeCmd.Flag("prefix", "public URL prefix").String()
	galeHttp   = galeCmd.Flag("http", "HTTP host:port").Default(":5000").String()
)

func galeFn() error {
	prefix := *galePrefix
	addr := *galeHttp
	template, err := ioutil.ReadFile("scripts/main.html")
	if err != nil {
		return err
	}
	http.HandleFunc(prefix+"/", func(w http.ResponseWriter, req *http.Request) {
		handleGaleWarnings(*galeDir, template, w, req)
	})
	http.Handle(prefix+"/scripts/", http.StripPrefix(prefix+"/scripts/",
		http.FileServer(http.Dir("scripts"))))
	return http.ListenAndServe(addr, nil)
}
