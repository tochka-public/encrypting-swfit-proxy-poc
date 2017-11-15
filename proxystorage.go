package main

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

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

type authTransport struct {
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// fmt.Println("--Request--")
	// requestDump, err := httputil.DumpRequest(req, true)
	// if err != nil {
	// 	fmt.Println(err)
	// }
	// fmt.Println(string(requestDump))
	// fmt.Println("----Response----")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if resp.Header.Get("X-Storage-Url") == "https://134225.selcdn.ru/" {
		resp.Header.Set("X-Storage-Url", "http://localhost:9091")
	}
	// responseDump, err := httputil.DumpResponse(resp, true)
	// if err != nil {
	// 	fmt.Println(err)
	// }
	// fmt.Println(string(responseDump))

	return resp, err
}

type storageTransport struct {
}

func (t *storageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	fmt.Println("--Request--")
	// if req.ContentLength > 0 {
	// 	fmt.Println("================ we have a body, it's size is:", req.ContentLength)
	// }
	requestDump, err := httputil.DumpRequest(req, false)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(string(requestDump))
	// PUT, something in body and URI contains more than one slash and
	// not a slash at the end — probably a file upload
	if req.Method == "PUT" && req.ContentLength > 0 &&
		strings.LastIndex(req.RequestURI, "/") != 0 &&
		strings.LastIndex(req.RequestURI, "/") < len(req.RequestURI)-1 {
		fmt.Println("probably a file upload, we should encode")
	} else if req.Method == "GET" &&
		strings.LastIndex(req.RequestURI, "/") != 0 &&
		strings.LastIndex(req.RequestURI, "/") < len(req.RequestURI)-1 {
		fmt.Println("probably a file download, we should encode")
	}
	fmt.Println("----Response----")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if resp.ContentLength > 0 {
		fmt.Println("================ we have a body, it's size is:", resp.ContentLength)
	}
	responseDump, err := httputil.DumpResponse(resp, false)
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println(string(responseDump))

	return resp, err
}

// MyReverseProxy — updated ReverseProxy with redefined director
func MyReverseProxy(target *url.URL) *httputil.ReverseProxy {
	targetQuery := target.RawQuery
	director := func(req *http.Request) {
		target := target
		req.Host = target.Host
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = singleJoiningSlash(target.Path, req.URL.Path)
		if targetQuery == "" || req.URL.RawQuery == "" {
			req.URL.RawQuery = targetQuery + req.URL.RawQuery
		} else {
			req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
		}

	}

	return &httputil.ReverseProxy{
		Director: director,
	}
}

func main() {
	authproxy := MyReverseProxy(&url.URL{
		Scheme: "https",
		Host:   "auth.selcdn.ru",
	})
	authproxy.Transport = &authTransport{}

	storageproxy := MyReverseProxy(&url.URL{
		Scheme: "https",
		Host:   "134225.selcdn.ru",
	})
	storageproxy.Transport = &storageTransport{}

	go func() {
		http.ListenAndServe(":9090", authproxy)
	}()
	http.ListenAndServe(":9091", storageproxy)
}
