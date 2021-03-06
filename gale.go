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
	"sort"
	"strconv"
	"strings"
	"time"
)

type GaleWarning struct {
	Number int
	Date   time.Time
}

// Bulletin spécial: Avis de Grand frais à Coup de vent numéro 36
var (
	reWarning = regexp.MustCompile(`^\s*(?:Bulletin spécial:|BMS\s+côte\s+numéro).*?(\d+)`)
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
	rePath = regexp.MustCompile(`^.*(\d{4}_\d{2}_\d{2}T_?\d{2}_\d{2}_\d{2})\.txt$`)
)

type sortedWarnings []GaleWarning

func (s sortedWarnings) Len() int {
	return len(s)
}

func (s sortedWarnings) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s sortedWarnings) Less(i, j int) bool {
	return s[i].Date.Before(s[j].Date)
}

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
		date := strings.Replace(m[1], "T_", "T", -1)
		d, err := time.Parse("2006_01_02T15_04_05", date)
		if err != nil {
			return err
		}
		n, err := extractWarningNumber(path)
		if err != nil {
			return err
		}
		warnings = append(warnings, GaleWarning{
			Number: n,
			Date:   d,
		})
		return nil
	})
	sort.Sort(sortedWarnings(warnings))
	// Fill intermediary reports without warnings with previous warning number
	num := 1
	for i, w := range warnings {
		if w.Number != 0 {
			num = w.Number
		} else {
			w := w
			w.Number = num
			warnings[i] = w
		}
	}
	return warnings, err
}

func serveGaleWarnings(galeDir string, template []byte, w http.ResponseWriter,
	req *http.Request) error {

	warnings, err := extractWarningNumbers(galeDir)
	if err != nil {
		return err
	}
	// Add virtual beginning of year and current day points
	now := time.Now()
	jan1 := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
	if len(warnings) == 0 || jan1.Before(warnings[0].Date) {
		warnings = append([]GaleWarning{GaleWarning{
			Number: 0,
			Date:   jan1,
		}}, warnings...)
	}
	warnings = append(warnings, GaleWarning{
		Number: warnings[len(warnings)-1].Number,
		Date:   now,
	})

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
	fmt.Printf("serving on %s\n", addr)
	return http.ListenAndServe(addr, nil)
}
