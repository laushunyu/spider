package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/PuerkitoBio/goquery"
	"github.com/fatih/color"
	htmlPkg "github.com/laushunyu/spider/html"
	log "github.com/sirupsen/logrus"
	"go.uber.org/multierr"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

var client = http.DefaultClient

type ListPage struct {
	url *url.URL
	doc *goquery.Document
}

func (l *ListPage) Artifacts() ([]Artifact, error) {
	// now list page arts
	artsDom := l.doc.Find(".card > .container > .columns")

	log.Infof("find %d art in page", artsDom.Size())

	var arts []Artifact
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
		tu, err := l.url.Parse(torrentPath)
		if err != nil {
			log.WithField("path", torrentPath).Warning("failed to parse torrent path")
			return
		}
		art.TorrentUrl = tu.String()

		// debug
		log.Debugf("get art %+v", art)

		arts = append(arts, art)
	})
	return arts, nil
}

func (l *ListPage) HasNext(ctx context.Context) bool {
	lastPagination := l.doc.Find(".pagination-list li:last-child > a")
	return !lastPagination.HasClass("is-inverted")
}

var (
	ErrorIsLastPage = errors.New("no more pages")
)

func (l *ListPage) Next(ctx context.Context) (*ListPage, error) {
	if l.HasNext(ctx) {
		return nil, ErrorIsLastPage
	}

	nextUrl := *l.url
	query := nextUrl.Query()

	page := 1
	if pageParam := query.Get("page"); pageParam != "" {
		p, err := strconv.Atoi(pageParam)
		if err != nil {
			return nil, err
		}
		page = p
	}
	// gen next page query param, next page is (i + 1)
	page++

	query.Set("page", strconv.Itoa(page))
	nextUrl.RawQuery = query.Encode()

	return GetListPage(ctx, nextUrl.String())
}

func GetListPage(ctx context.Context, url string) (*ListPage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	return &ListPage{
		url: req.URL,
		doc: doc,
	}, nil
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
	log.Debugf("downloading %s %s", a.ID, a.Name)
	defer log.Infof("success downloaded %s", color.GreenString("%s %s", a.ID, a.Name))

	dir = filepath.Join(dir, a.ID)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return err
	}

	// save metadata
	metaRaw, err := json.MarshalIndent(a, "", "\t")
	if err != nil {
		return err
	}

	// download torrent
	_, torrFileName := path.Split(a.TorrentUrl)
	_, imgFileName := path.Split(a.ImageUrl)

	return multierr.Combine(
		ioutil.WriteFile(path.Join(dir, "metadata.json"), metaRaw, 0644),
		htmlPkg.DoGetDownload(path.Join(dir, torrFileName), a.TorrentUrl),
		htmlPkg.DoGetDownload(path.Join(dir, imgFileName), a.ImageUrl),
		func() error {
			if len(a.ExtraImageUrl) == 0 {
				return nil
			}

			dir := path.Join(dir, "extrafanart")
			if err := os.MkdirAll(dir, os.ModePerm); err != nil {
				return err
			}

			var err error
			for _, imageUrl := range a.ExtraImageUrl {
				_, file := path.Split(imageUrl)
				err = multierr.Append(err, htmlPkg.DoGetDownload(path.Join(dir, file), imageUrl))
			}
			return err
		}(),
	)
}
