package main

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"
)

type transport struct {
	http.RoundTripper
	breaker *ExtendedCircuitBreakerMeta
	context *gin.Context
}

func (t *transport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	if t.breaker.CB.Ready() {
		log.Debug("ON REQUEST: Breaker status: ", t.breaker.CB.Ready())
		resp, err = t.RoundTripper.RoundTrip(req)

		if err != nil {
			log.Error("Circuit Breaker Failed")
			t.breaker.CB.Fail()
		} else if resp.StatusCode == 500 {
			t.breaker.CB.Fail()
		} else {
			t.breaker.CB.Success()

			//This is useful for the middlewares
			var bodyBytes []byte

			if resp.Body != nil {
				defer resp.Body.Close()
				bodyBytes, _ = ioutil.ReadAll(resp.Body)
			}

			// Restore the io.ReadCloser to its original state
			resp.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))

			// Use the content
			log.WithFields(log.Fields{
				"req":  req,
				"resp": resp,
			}).Info("Setting body")

			t.context.Set("body", bodyBytes)
		}
	}

	return resp, err
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

// NewSingleHostReverseProxy returns a new ReverseProxy that routes
// URLs to the scheme, host, and base path provided in target. If the
// target's path is "/base" and the incoming request was for "/dir",
// the target request will be for /base/dir.
// NewSingleHostReverseProxy does not rewrite the Host header.
// To rewrite Host headers, use ReverseProxy directly with a custom
// Director policy.
func NewSingleHostReverseProxy(proxy Proxy, transport http.RoundTripper) *httputil.ReverseProxy {
	target, _ := url.Parse(proxy.TargetURL)
	targetQuery := target.RawQuery

	director := func(req *http.Request) {
		listenPath := strings.Replace(proxy.ListenPath, "/*randomName", "", -1)
		path := singleJoiningSlash(target.Path, req.URL.Path)

		if proxy.StripListenPath {
			log.Debugf("Stripping: %s", proxy.ListenPath)
			path = strings.Replace(path, listenPath, "", 1)
		}

		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = path
		req.Host = target.Host

		if targetQuery == "" || req.URL.RawQuery == "" {
			req.URL.RawQuery = targetQuery + req.URL.RawQuery
		} else {
			req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
		}
	}

	return &httputil.ReverseProxy{Director: director, Transport: transport}
}