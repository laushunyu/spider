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
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/fatih/color"
	htmlPkg "github.com/laushunyu/spider/html"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"go.uber.org/multierr"
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
	log.Debugf("downloading artifact metainfo %s %s", a.ID, a.Name)

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

		wg := sync.WaitGroup{}
		var errRet error
		for _, u := range a.ExtraImageUrl {
			wg.Add(1)
			imageUrl := u
			_, file := path.Split(imageUrl)
			go func() {
				defer wg.Done()
				errRet = multierr.Append(errRet, htmlPkg.DoGetDownload(path.Join(dir, file), imageUrl))
			}()
		}
		if wg.Wait(); errRet != nil {
			return err
		}
	}

	log.Infof("success to download artifact metainfo %s", color.GreenString("%s %s", a.ID, a.Name))
	return nil
}

func (w *Website) DownloadAllArtifactsByPopularTo(timeRange int, limit int, artsDir string) error {
	// mkdir
	if err := os.MkdirAll(artsDir, os.ModePerm); err != nil {
		log.Fatalln(err)
	}

	var arts []Artifact

	// get cache from metadata.json
	mdPath := filepath.Join(artsDir, "metadata.json")

	mdRaw, err := ioutil.ReadFile(mdPath)
	if err == nil {
		// file exist, do unmarshal
		if err := json.Unmarshal(mdRaw, &arts); err != nil {
			// unmarshal fail, should download from remote
			log.WithError(err).Warning("metadata broken, will download from remote website page")
			err = os.ErrNotExist
		}
		log.Debugf("load list metadata.json cache success")
	}
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		// not exist, download from website page

		// get artifacts of specify date
		a, err := w.GetArtifactsByPopular(timeRange, limit)
		if err != nil {
			log.Fatalln(err)
		}
		arts = a

		artsRaw, err := json.MarshalIndent(a, "", "\t")
		if err != nil {
			return err
		}
		if err := ioutil.WriteFile(mdPath, artsRaw, 0644); err != nil {
			return err
		}
	}

	// download arts concurrently
	log.Debugf("start downloading %d arts", len(arts))
	var errRet error
	wg, sema := sync.WaitGroup{}, semaphore.NewWeighted(concurrent)
	for i := range arts {
		wg.Add(1)
		art := arts[i]

		go func() {
			defer wg.Done()

			_ = sema.Acquire(context.TODO(), 1)
			defer sema.Release(1)

			errRet = multierr.Append(errRet, art.DownloadTo(filepath.Join(artsDir, art.ID)))
		}()
	}

	if wg.Wait(); errRet != nil {
		return err
	}

	return nil
}

func (w *Website) DownloadAllArtifactsByDateTo(y, m, d int, artsDir string) error {
	// mkdir
	if err := os.MkdirAll(artsDir, os.ModePerm); err != nil {
		log.Fatalln(err)
	}

	var arts []Artifact

	// get cache from metadata.json
	mdPath := filepath.Join(artsDir, "metadata.json")

	mdRaw, err := ioutil.ReadFile(mdPath)
	if err == nil {
		// file exist, do unmarshal
		if err := json.Unmarshal(mdRaw, &arts); err != nil {
			// unmarshal fail, should download from remote
			log.WithError(err).Warning("metadata broken, will download from remote website page")
			err = os.ErrNotExist
		}
		log.Debugf("load list metadata.json cache success")
	}
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		// not exist, download from website page

		// get artifacts of specify date
		a, err := w.GetArtifactsByDate(y, m, d, -1)
		if err != nil {
			log.Fatalln(err)
		}
		arts = a

		artsRaw, err := json.MarshalIndent(a, "", "\t")
		if err != nil {
			return err
		}
		if err := ioutil.WriteFile(mdPath, artsRaw, 0644); err != nil {
			return err
		}
	}

	// download arts concurrently
	log.Debugf("start downloading %d arts", len(arts))
	var errRet error
	wg, sema := sync.WaitGroup{}, semaphore.NewWeighted(concurrent)
	for i := range arts {
		wg.Add(1)
		art := arts[i]

		go func() {
			defer wg.Done()

			_ = sema.Acquire(context.TODO(), 1)
			defer sema.Release(1)

			errRet = multierr.Append(errRet, art.DownloadTo(filepath.Join(artsDir, art.ID)))
		}()
	}

	if wg.Wait(); errRet != nil {
		return err
	}

	return nil
}

func (w *Website) GetArtifactsByPopular(timeRange int, limit int) (arts []Artifact, err error) {
	if limit < 0 && limit > 50 {
		// max 10 page
		limit = 50
	}

	u, err := w.baseUrl.Parse(fmt.Sprintf("popular/%d", timeRange))
	if err != nil {
		return nil, err
	}

	for i := 1; i <= 5; i++ {
		log.Infof("get page %s", u.String())
		respBody, err := htmlPkg.DoGet(u.String())
		if err != nil {
			return arts, err
		}

		log.Infof("process page %s", u.String())
		art, next, err := w.GetArtifactsFromHtml(respBody)
		_ = respBody.Close() // close body
		if err != nil {
			return arts, err
		}
		arts = append(arts, art...)

		if len(arts) >= limit {
			return arts, nil
		}

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
		_ = respBody.Close() // close body
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

func fnTime(website *Website, dataStr string) error {
	var err error

	if dataStr == "now" {
		date = time.Now()
	} else {
		date, err = time.Parse("2006-1-2", dataStr)
		if err != nil {
			return err
		}
	}

	// output/2022/3/11
	y, m, d := date.Date()
	artsDir := filepath.Join(output, strconv.Itoa(y), strconv.Itoa(int(m)), strconv.Itoa(d))
	if err := website.DownloadAllArtifactsByDateTo(y, int(m), d, artsDir); err != nil {
		return err
	}
	return nil
}

func fnPopular(website *Website, timeRangeStr, countStr string) error {
	if timeRangeStr == "" {
		timeRangeStr = "7" // 最近七天
	}
	if countStr == "" {
		countStr = "50" // top 50
	}

	timeRange, err := strconv.Atoi(timeRangeStr)
	if err != nil {
		return errors.Errorf("unknown time range %s", timeRangeStr)
	}
	if timeRange != 7 && timeRange != 30 && timeRange != 60 {
		return errors.New("time range must be one of (60, 30, 7)")
	}
	count, err := strconv.Atoi(countStr)
	if err != nil {
		return errors.Errorf("unknown count %s", countStr)
	}

	artsDir := filepath.Join(output, fmt.Sprintf("last-%d-top-%d", timeRange, count))
	if err := website.DownloadAllArtifactsByPopularTo(timeRange, count, artsDir); err != nil {
		return err
	}
	return nil
}

func main() {
	_, err := net.LookupHost(host)
	if err != nil {
		log.WithError(err).Fatalln("unknown host")
	}
	website := NewWebsite(host)

	fn := flag.Arg(0)

	switch fn {
	case "time":
		// by time
		if err := fnTime(website, flag.Arg(1)); err != nil {
			log.Fatalln(err)
		}
	case "tag":
		// by tag

	case "popular":
		// by popular
		if err := fnPopular(website, flag.Arg(1), flag.Arg(2)); err != nil {
			log.Fatalln(err)
		}
	default:
		// default by time
		if err := fnTime(website, flag.Arg(0)); err != nil {
			log.Fatalln(err)
		}
	}

	log.Info("success!")
}
