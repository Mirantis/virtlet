package framework

import (
	"io/ioutil"
	"net/http"
)

func Curl(url string) (string, error) {
	client := &http.Client{Transport: &http.Transport{}}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body[:]), nil
}
