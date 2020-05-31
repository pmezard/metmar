package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"

	httpgzip "github.com/daaku/go.httpgzip"
)

func hashReport(report string) string {
	h := sha256.Sum256([]byte(report))
	return hex.EncodeToString(h[:])
}

func httpGet(url string, headers map[string]string) (io.ReadCloser, error) {
	rq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		rq.Header.Set(k, v)
	}
	rq.Header.Set("User-Agent", "Mozilla/4.0 (compatible; MSIE 7.0; Windows NT 6.0)")
	rsp, err := http.DefaultClient.Do(rq)
	if err != nil {
		return nil, err
	}
	if rsp.StatusCode != http.StatusOK {
		rsp.Body.Close()
		return nil, fmt.Errorf("got %d fetching %s", rsp.StatusCode, url)
	}
	return rsp.Body, nil
}

type Region struct {
	Title       string `json:"titreRegion"`
	Situation   string
	Observation string
	WindAndSea  string `json:"ventEtMer"`
	Swell       string `json:"houle"`
	Weather     string `json:"ts"`
	Visibility  string `json:"visi"`
}

type Echeance struct {
	Title   string   `json:"titreEcheance"`
	Kind    string   `json:"nomEcheance"`
	Regions []Region `json:"region"`
}

type Report struct {
	Title     string     `json:"titreBulletin"`
	Special   string     `json:"bulletinSpecial"`
	Header    string     `json:"chapeauBulletin"`
	Footer    string     `json:"piedBulletin"`
	Units     string     `json:"uniteBulletin"`
	Echeances []Echeance `json:"echeance"`
}

func jsonGet(url string) ([]*Report, error) {
	headers := map[string]string{}
	r, err := httpGet(url, headers)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	reports := []*Report{}
	err = json.NewDecoder(r).Decode(&reports)
	return reports, err
}

type Forecast struct {
	Id      string
	Title   string
	Content string
}

var (
	reLines = regexp.MustCompile(`\n+`)
)

func htmlToText(html string) string {
	s := strings.Replace(html, "<br />", "\n", -1)
	s = strings.TrimSpace(s)
	s = reLines.ReplaceAllString(s, "\n")
	return s
}

func formatReport(reports []*Report) (*Forecast, error) {
	if len(reports) != 2 {
		return nil, fmt.Errorf("2 reports expected, go %d", len(reports))
	}
	// Coastal report
	r := reports[1]
	content := []string{}
	content = append(content, r.Title, "\n\n")
	content = append(content, htmlToText(r.Header), "\n")
	content = append(content, htmlToText(r.Footer), "\n\n")
	content = append(content, htmlToText(r.Special), "\n\n")
	for _, e := range r.Echeances {
		content = append(content, "# ", e.Title, "\n\n")
		for _, a := range e.Regions {
			parts := []string{
				a.Situation,
				a.Observation,
				a.WindAndSea,
				a.Swell,
				a.Weather,
				a.Visibility,
			}
			for _, part := range parts {
				if part == "" {
					continue
				}
				part = htmlToText(part)
				part = strings.TrimSpace(part)
				content = append(content, part, "\n")
			}
		}
		content = append(content, "\n\n")
	}
	return &Forecast{
		Title:   r.Title,
		Content: strings.Join(content, ""),
	}, nil
}

func fetchForecasts() ([]Forecast, error) {
	urlFmt := "http://www.meteofrance.com/mf3-rpc-portlet/rest/bulletins/cote/%d/bulletinsMarineMetropole"
	forecasts := []Forecast{}
	for i := 1; i <= 9; i++ {
		url := fmt.Sprintf(urlFmt, i)
		reports, err := jsonGet(url)
		if err != nil {
			return nil, err
		}
		forecast, err := formatReport(reports)
		if err != nil {
			return nil, err
		}
		forecast.Id = strconv.FormatInt(int64(i), 10)
		forecasts = append(forecasts, *forecast)
	}
	return forecasts, nil
}

const (
	htmlTemplate = `<html>
<head>
	<title>Marine weather forecasts in Brest area</title>
</head>
<body>
	{{range .}}
		<a href="{{.URL}}">{{.Name}}</a><br/>
	{{end}}
</body>
</html>
`
)

func formatAreas(t *template.Template, forecasts []Forecast) (string, error) {
	type Area struct {
		URL  string
		Name string
	}
	data := []Area{}
	for _, forecast := range forecasts {
		data = append(data, Area{
			URL:  "areas/" + forecast.Id,
			Name: forecast.Title,
		})
	}
	w := &bytes.Buffer{}
	err := t.Execute(w, data)
	if err != nil {
		return "", err
	}
	return w.String(), nil
}

func renderAreas(t *template.Template) (string, error) {
	forecasts, err := fetchForecasts()
	if err != nil {
		return "", err
	}
	return formatAreas(t, forecasts)
}

func serveAreas(t *template.Template, w http.ResponseWriter, req *http.Request) {
	areas, err := renderAreas(t)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain;charset=utf-8")
		w.WriteHeader(500)
		fmt.Fprintf(w, "error: %s\n", err)
		return
	}
	w.Header().Set("Content-Type", "text/html;charset=utf-8")
	h := hashReport(areas)
	w.Header().Set("ETag", h)
	etag := req.Header.Get("If-None-Match")
	if etag == h {
		w.WriteHeader(304)
		return
	}
	fmt.Fprintf(w, "%s", areas)
}

func renderForecast(id string) (string, error) {
	forecasts, err := fetchForecasts()
	if err != nil {
		return "", err
	}
	forecast := Forecast{}
	for _, f := range forecasts {
		if f.Id == id {
			forecast = f
			break
		}
	}
	if forecast.Id == "" {
		return "", fmt.Errorf("cannot find forecast: %s", id)
	}
	return forecast.Content, nil
}

func serveForecast(w http.ResponseWriter, req *http.Request) {
	id := path.Base(req.URL.Path)
	report, err := renderForecast(id)
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

var (
	serveCmd    = app.Command("serve", "reformat forecasts and serve them over HTTP")
	servePrefix = serveCmd.Flag("prefix", "public URL prefix").String()
	serveHttp   = serveCmd.Flag("http", "HTTP host:port").Default(":5000").String()
)

func serveFn() error {
	prefix := *servePrefix
	addr := *serveHttp
	t, err := template.New("areas").Parse(htmlTemplate)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc(prefix+"/", func(w http.ResponseWriter, req *http.Request) {
		serveAreas(t, w, req)
	})
	mux.HandleFunc(prefix+"/areas/", serveForecast)
	fmt.Printf("serving on %s\n", addr)
	return http.ListenAndServe(addr, httpgzip.NewHandler(mux))
}

var (
	parseCmd = app.Command("parse",
		"fetch and parse current forecast, for debugging purpose")
	parseId = parseCmd.Arg("id", "forecast identifier").Required().String()
)

func parseFn() error {
	forecastId := *parseId
	text, err := renderForecast(forecastId)
	if err != nil {
		return err
	}
	fmt.Println(text)
	return nil
}
