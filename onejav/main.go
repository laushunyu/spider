package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	htmlPkg "github.com/laushunyu/spider/html"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

var (
	concurrent int64
	host       string
	date       time.Time
	level      string
	output     string
)

func init() {
	flag.Int64Var(&concurrent, "p", 1, "concurrent of process")
	flag.StringVar(&host, "h", host, "target website host")
	flag.StringVar(&level, "level", "info", "log level")
	flag.StringVar(&output, "o", "output", "output dir")
	flag.Parse()

	// set log level
	l, err := log.ParseLevel(level)
	if err != nil {
		log.Fatalln(err)
	}
	log.SetLevel(l)
	log.Debugf("set log level as %s", l)

	// parse arg
	dateStr := flag.Arg(0)
	if dateStr == "now" {
		date = time.Now()
	} else {
		date, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			log.Fatalln(err)
		}
	}
}

type Website struct {
	baseUrl *url.URL
}

func NewWebsite(host string) *Website {
	return &Website{
		baseUrl: &url.URL{
			Scheme: "https",
			Host:   host,
			Path:   "/",
		},
	}
}

type Artifact struct {
	ID            string
	Name          string
	Size          string
	Time          string
	TorrentUrl    string
	Tag           []string
	Actress       []string
	ImageUrl      string
	ExtraImageUrl []string
}

func (a *Artifact) DownloadTo(dir string) error {
	log.Infof("downloading artifact metainfo %s %s", a.ID, a.Name)

	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return err
	}

	// save metadata
	metaRaw, err := json.MarshalIndent(a, "", "\t")
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(path.Join(dir, "metadata.json"), metaRaw, 0644); err != nil {
		return err
	}

	// download torrent
	_, torrFileName := path.Split(a.TorrentUrl)
	if err := htmlPkg.DoGetDownload(path.Join(dir, torrFileName), a.TorrentUrl); err != nil {
		return err
	}

	// download thumb image
	_, imgFileName := path.Split(a.ImageUrl)
	if err := htmlPkg.DoGetDownload(path.Join(dir, imgFileName), a.ImageUrl); err != nil {
		return err
	}

	// download extra image
	if len(a.ExtraImageUrl) != 0 {
		dir := path.Join(dir, "extrafanart")
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return err
		}

		eg := errgroup.Group{}
		for _, u := range a.ExtraImageUrl {
			imageUrl := u
			_, file := path.Split(imageUrl)
			eg.Go(func() error {
				return htmlPkg.DoGetDownload(path.Join(dir, file), imageUrl)
			})
		}
		if err := eg.Wait(); err != nil {
			return err
		}
	}

	return nil
}

func (w *Website) GetArtifactsByDate(y, m, d int, pageLimit int) (arts []Artifact, err error) {
	if pageLimit < 0 {
		pageLimit = math.MaxInt
	}

	u, err := w.baseUrl.Parse(fmt.Sprintf("%4d/%02d/%02d", y, m, d))
	if err != nil {
		return nil, err
	}

	for i := 1; i <= pageLimit; i++ {
		log.Infof("get page %s", u.String())
		respBody, err := htmlPkg.DoGet(u.String())
		if err != nil {
			return arts, err
		}

		log.Infof("process page %s", u.String())
		art, next, err := w.GetArtifactsFromHtml(respBody)
		respBody.Close() // close body
		if err != nil {
			return arts, err
		}
		arts = append(arts, art...)

		// if last page then ret
		if !next {
			log.Infof("find %d arts in list page %s", len(arts), u.String())
			return arts, nil
		}

		// gen next page query param, next page is (i + 1)
		param := u.Query()
		param.Set("page", strconv.Itoa(i+1))
		u.RawQuery = param.Encode()
	}

	return
}

func (w *Website) GetArtifactsFromHtml(reader io.Reader) (arts []Artifact, next bool, err error) {
	doc, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return nil, false, err
	}

	// now list page arts
	artsDom := doc.Find(".card > .container > .columns")

	log.Infof("find %d art in page", artsDom.Size())

	// for each artifact in list
	artsDom.Each(func(i int, selection *goquery.Selection) {
		art := Artifact{}

		// each image in images
		selection.Find(".column img").Each(func(i int, selection *goquery.Selection) {
			src, ok := selection.Attr("src")
			if !ok {
				raw, _ := selection.Html()
				log.WithField("html", raw).Warning("found img item without src attr")
				return
			}

			if i == 0 {
				art.ImageUrl = src
				return
			}

			art.ExtraImageUrl = append(art.ExtraImageUrl, src)
		})

		// detail
		detailDom := selection.Find(".card-content")

		// id and size
		titleDom := detailDom.Find(".title")
		art.ID = strings.TrimSpace(titleDom.Find("a").Text())
		art.Size = strings.TrimSpace(titleDom.Find("span").Text())

		// time
		timePath := detailDom.Find(".subtitle a").AttrOr("href", "/1970/01/01")
		art.Time = strings.Trim(strings.ReplaceAll(timePath, "/", "-"), "-")

		// tag
		detailDom.Find(".tags a").Each(func(i int, selection *goquery.Selection) {
			art.Tag = append(art.Tag, strings.TrimSpace(selection.Text()))
		})

		// title
		art.Name = strings.TrimSpace(detailDom.Find(".level").Text())

		// url
		torrentPath, ok := detailDom.Find(".field > .control > a").Attr("href")
		if !ok {
			log.WithField("art", art).Warning("cannot found art torrent path, skip")
			return
		}
		tu, err := w.baseUrl.Parse(torrentPath)
		if err != nil {
			log.WithField("base", w.baseUrl.String()).
				WithField("path", torrentPath).
				Warning("failed to parse torrent path")
			return
		}
		art.TorrentUrl = tu.String()

		// debug
		log.Debugf("get art %+v", art)

		arts = append(arts, art)
	})

	// if it has next page
	lastPagination := doc.Find(".pagination-list li:last-child > a")
	next = lastPagination.HasClass("is-inverted")

	return
}

func main() {
	_, err := net.LookupHost(host)
	if err != nil {
		log.WithError(err).Fatalln("unknown host")
	}

	website := NewWebsite(host)

	y, m, d := date.Date()

	// get artifacts of specify date
	arts, err := website.GetArtifactsByDate(y, int(m), d, -1)
	if err != nil {
		panic(err)
	}

	// download arts concurrently
	eg, sema := errgroup.Group{}, semaphore.NewWeighted(concurrent)
	for i := range arts {
		art := arts[i]

		eg.Go(func() error {
			sema.Acquire(context.TODO(), 1)
			defer sema.Release(1)

			return art.DownloadTo(filepath.Join(output, strconv.Itoa(y), strconv.Itoa(int(m)), strconv.Itoa(d), art.ID))
		})
	}

	if err := eg.Wait(); err != nil {
		log.Fatalln(err)
	}
}
