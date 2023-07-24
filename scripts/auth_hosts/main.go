package main

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

func log(s string, args ...any) {
	os.Stderr.WriteString(fmt.Sprintf(s, args...) + "\n")
}

const HOST = "ocapi-app.arlo.com"

func main() {
	log("Starting playwright")
	pw, err := playwright.Run()
	if err != nil {
		panic(err)
	}

	log("Launching browser")
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	if err != nil {
		panic(err)
	}

	log("Navigating to page")
	page, err := browser.NewPage()
	if err != nil {
		panic(err)
	}
	if _, err = page.Goto("https://search.censys.io/"); err != nil {
		panic(err)
	}

	time.Sleep(5 * time.Second)

	log("Querying for host")
	err = page.Fill("#q", HOST)
	if err != nil {
		panic(err)
	}

	submit, err := page.QuerySelector("#submit-button")
	if err != nil {
		panic(err)
	}
	submit.Click()

	time.Sleep(10 * time.Second)

	log("Parsing response")
	results, err := page.QuerySelectorAll(".SearchResult > a")
	if err != nil {
		panic(err)
	}

	log("Got %d results", len(results))

	ips := []string{}
	for _, result := range results {
		txt, err := result.InnerText()
		if err != nil {
			log(fmt.Sprintf("could not extract text: %s", err))
			continue
		}

		txt = strings.TrimSpace(txt)
		tokens := strings.Split(txt, " ")
		ip := tokens[0]

		if err := verifyRemoteCert(ip); err != nil {
			log("ip %s cannot be verified: %s", ip, err)
			continue
		}

		if err := verifyRemoteAuthEndpoint(ip); err != nil {
			log("ip %s auth endpoint cannot be used: %s", ip, err)
			continue
		}

		ips = append(ips, ip)
	}

	log("Writing results")
	enc := json.NewEncoder(os.Stdout)
	err = enc.Encode(ips)
	if err != nil {
		panic(err)
	}
	enc = json.NewEncoder(os.Stderr)
	err = enc.Encode(convertB64(ips))
	if err != nil {
		panic(err)
	}
}

func verifyRemoteCert(ip string) error {
	conf := &tls.Config{
		InsecureSkipVerify: true,
	}

	conn, err := tls.Dial("tcp", ip+":443", conf)
	if err != nil {
		return err
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	for _, cert := range certs {
		if err := cert.VerifyHostname(HOST); err == nil {
			return nil
		}
	}

	return fmt.Errorf("remote certs do not match hostname")
}

func verifyRemoteAuthEndpoint(ip string) error {
	t := http.Transport{
		TLSClientConfig: &tls.Config{
			ServerName: HOST,
		},
		TLSHandshakeTimeout: 10 * time.Second,
	}
	c := http.Client{
		Transport: &t,
		Timeout:   5 * time.Second,
	}

	resp, err := c.Post(fmt.Sprintf("https://%s/api/auth", ip), "", nil)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	return nil
}

func convertB64(s []string) []string {
	result := []string{}
	for _, s := range s {
		result = append(result, base64.StdEncoding.EncodeToString([]byte(s)))
	}
	return result
}
