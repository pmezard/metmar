package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"path"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/transform"

	"github.com/PuerkitoBio/goquery"
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

func htmlGet(url string, headers map[string]string, enc encoding.Encoding) (
	*goquery.Document, error) {

	r, err := httpGet(url, headers)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var reader io.Reader = r
	if enc != nil {
		reader = transform.NewReader(reader, enc.NewDecoder())
	}
	//reader = &LogReader{r}
	return goquery.NewDocumentFromReader(reader)
}

type Forecast struct {
	Id      string
	Title   string
	Content string
}

func htmlToText(spans *goquery.Selection) string {
	var buf bytes.Buffer

	for i := range spans.Nodes {
		span := spans.Eq(i)
		class, _ := span.Attr("class")
		if class == "titre2" || class == "titre3" {
			buf.WriteString("\n")
		}
		buf.WriteString(strings.TrimSpace(span.Text()) + "\n")
	}
	return buf.String()
}

func parseWeather(doc *goquery.Document) []Forecast {
	forecasts := []Forecast{}
	zones := doc.Find("div[class='affichebulletins']")
	for i := range zones.Nodes {
		zone := zones.Eq(i)
		id, _ := zone.Attr("id")
		title := zone.Find("h2").First().Text()
		contentSel := zone.Find("span")
		content := htmlToText(contentSel)
		forecasts = append(forecasts, Forecast{
			Id:      id,
			Title:   title,
			Content: content,
		})
	}
	return forecasts
}

func fetchForecasts() ([]Forecast, error) {
	url := "http://vigiprevi.meteofrance.com/data/VBFR02_LFPW_.txt"
	headers := map[string]string{}
	doc, err := htmlGet(url, headers, nil)
	if err != nil {
		return nil, err
	}
	forecasts := parseWeather(doc)
	if len(forecasts) <= 0 {
		return nil, fmt.Errorf("no forecast found")
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
