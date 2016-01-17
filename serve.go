package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path"
	"strings"

	"github.com/daaku/go.httpgzip"
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

func getJson(url string, output interface{}) error {
	rsp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("could not get %s: %s", url, err)
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != 200 {
		return fmt.Errorf("failed to get %s: %s", url, rsp.Status)
	}
	err = json.NewDecoder(rsp.Body).Decode(output)
	if err != nil {
		return fmt.Errorf("could not decode json response for %s: %s", url, err)
	}
	return nil
}

func fetchWeather(id string) (string, error) {
	url := "http://www.meteofrance.com/mf3-rpc-portlet/rest/bulletins/cote/" + id +
		"/bulletinsMarineMetropole"
	reports := []*Bulletin{}
	err := getJson(url, &reports)
	if err != nil {
		return "", err
	}
	if len(reports) <= 0 {
		return "", fmt.Errorf("no report retrieved")
	}
	return formatReport(reports[0]), nil
}

func formatJsonWeather(w http.ResponseWriter, req *http.Request) {
	id := path.Base(req.URL.Path)
	report, err := fetchWeather(id)
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

type CoastalArea struct {
	Id  string
	URL string
}

func fetchAreas() ([]CoastalArea, error) {
	type Area struct {
		Id  string `json:"id"`
		URL string `json:"url"`
	}

	type AreaGroup struct {
		Name  string `json:"name"`
		Areas []Area `json:"zones"`
	}

	url := "http://www.meteofrance.com/mf3-rpc-portlet/js/datas/bulletins_marine.json"
	groups := map[string][]AreaGroup{}
	err := getJson(url, &groups)
	if err != nil {
		return nil, err
	}
	group, ok := groups["bulletinsMarineMetropole"]
	if !ok {
		return nil, fmt.Errorf("cannot extract metropole areas")
	}
	areas := []CoastalArea{}
	for _, g := range group {
		if g.Name != "cote" {
			continue
		}
		for _, a := range g.Areas {
			areas = append(areas, CoastalArea{
				Id:  a.Id,
				URL: a.URL,
			})
		}
	}
	return areas, nil
}

const (
	html = `<html>
<body>
	{{range .}}
		<a href="{{.URL}}">{{.Name}}</a><br/>
	{{end}}
</body>
</html>
`
)

func formatAreas(t *template.Template, areas []CoastalArea) (string, error) {
	type Area struct {
		URL  string
		Name string
	}
	data := []Area{}
	for _, area := range areas {
		data = append(data, Area{
			URL:  "areas/" + area.Id,
			Name: area.URL,
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
	areas, err := fetchAreas()
	if err != nil {
		return "", err
	}
	return formatAreas(t, areas)
}

func formatJsonAreas(t *template.Template, w http.ResponseWriter, req *http.Request) {
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

var (
	serveCmd    = app.Command("serve", "reformat forecasts and serve them over HTTP")
	servePrefix = serveCmd.Flag("prefix", "public URL prefix").String()
	serveHttp   = serveCmd.Flag("http", "HTTP host:port").Default(":5000").String()
)

func serveFn() error {
	prefix := *servePrefix
	addr := *serveHttp
	t, err := template.New("areas").Parse(html)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc(prefix+"/", func(w http.ResponseWriter, req *http.Request) {
		formatJsonAreas(t, w, req)
	})
	mux.HandleFunc(prefix+"/areas/", formatJsonWeather)
	return http.ListenAndServe(addr, httpgzip.NewHandler(mux))
}
