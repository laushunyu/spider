package html

import (
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"net/http"
	"os"
)

var client = http.DefaultClient

func DoGet(url string, modifyReq ...func(req *http.Request)) (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	for _, mod := range modifyReq {
		mod(req)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		msg, err := ioutil.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}

		return nil, errors.New(fmt.Sprintf("%s: %s", resp.Status, msg))
	}
	return resp.Body, nil
}

func DoGetDownload(dstPath string, url string, modifyReq ...func(req *http.Request)) error {
	if _, err := os.Stat(dstPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		// file not exist
	} else {
		// overwrite or skip?
		// now is skipping this file
		log.Debugf("file %s existed, skip", dstPath)
		return nil
	}

	fd, err := os.Create(dstPath)
	if err != nil {
		return err
	}

	body, err := DoGet(url, modifyReq...)
	if err != nil {
		return err
	}
	defer func() {
		_ = body.Close()
	}()

	if _, err := io.Copy(fd, body); err != nil {
		return err
	}

	return nil
}

func WithCookie(k, v string) func(req *http.Request) {
	return func(req *http.Request) {
		req.AddCookie(&http.Cookie{
			Name:  k,
			Value: v,
		})
	}
}
