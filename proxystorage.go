package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

func encrypt(body []byte, key []byte) ([]byte, error) {
	c, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(c)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, body, nil), nil
}

func decrypt(encryptedbody []byte, key []byte) ([]byte, error) {
	c, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(c)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(encryptedbody) < nonceSize {
		return nil, errors.New("encryptedbody too short")
	}

	nonce, encryptedbody := encryptedbody[:nonceSize], encryptedbody[nonceSize:]
	return gcm.Open(nil, nonce, encryptedbody, nil)
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

type authTransport struct {
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	fmt.Println("--Request--")
	requestDump, err := httputil.DumpRequest(req, true)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(string(requestDump))
	fmt.Println("----Response----")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if resp.Header.Get("X-Storage-Url") == "https://134225.selcdn.ru/" {
		resp.Header.Set("X-Storage-Url", "http://localhost:9091")
	}
	responseDump, err := httputil.DumpResponse(resp, true)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(string(responseDump))

	return resp, err
}

type storageTransport struct {
}

func (t *storageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	key := []byte("AES256Key-32Characters1234567890")
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
	// kinda same for GET
	var originalEtag, encryptedEtag string
	if req.Method == "PUT" && req.ContentLength > 0 &&
		strings.LastIndex(req.RequestURI, "/") != 0 &&
		strings.LastIndex(req.RequestURI, "/") < len(req.RequestURI)-1 {
		fmt.Println("probably a file upload, we should encrypt body")
		body, err := ioutil.ReadAll(req.Body)
		if err != nil {
			panic(err)
		}
		hasher := md5.New()
		hasher.Write(body)
		originalEtag = hex.EncodeToString(hasher.Sum(nil))
		encryptedBody, err := encrypt(body, key)
		if err != nil {
			panic(err)
		}
		hasher = md5.New()
		hasher.Write(encryptedBody)
		encryptedEtag = hex.EncodeToString(hasher.Sum(nil))
		req.Body = ioutil.NopCloser(bytes.NewReader(encryptedBody))
		req.ContentLength = int64(len(encryptedBody))
		req.Header.Set("Content-Length", strconv.Itoa(len(encryptedBody)))
	}
	requestedRange := req.Header.Get("Range")
	if requestedRange != "" {
		requestedRange = strings.Split(requestedRange, "=")[1] // assume bytes
	}
	if req.Method == "GET" && strings.LastIndex(req.RequestURI, "/") != 0 &&
		strings.LastIndex(req.RequestURI, "/") < len(req.RequestURI)-1 &&
		requestedRange != "" {
		req.Header.Del("Range")
	}
	fmt.Println("----Response----")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if req.Method == "PUT" && resp.StatusCode == 201 &&
		strings.LastIndex(req.RequestURI, "/") != 0 &&
		strings.LastIndex(req.RequestURI, "/") < len(req.RequestURI)-1 &&
		encryptedEtag == resp.Header.Get("Etag") {
		resp.Header.Set("Etag", originalEtag)

	}
	if req.Method == "GET" && resp.ContentLength > 0 &&
		resp.StatusCode >= 200 && resp.StatusCode < 300 &&
		strings.LastIndex(req.RequestURI, "/") != 0 &&
		strings.LastIndex(req.RequestURI, "/") < len(req.RequestURI)-1 {
		fmt.Println("probably a file download, we should decrypt body")
		body, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			panic(err)
		}
		decryptedBody, err := decrypt(body, key)
		if err != nil {
			panic(err)
		}
		decryptedBodyLen := len(decryptedBody)
		hasher := md5.New()
		hasher.Write(decryptedBody)
		etagString := hex.EncodeToString(hasher.Sum(nil))
		if requestedRange != "" {
			ranges := strings.Split(requestedRange, "-")
			var from, to int
			if ranges[0] != "" {
				from, err = strconv.Atoi(ranges[0])
				if err != nil {
					panic(err)
				}
				from = from - 1
			} else {
				from = 0
			}
			if ranges[1] != "" {
				fmt.Println("ranges 1!", ranges[1])
				to, err = strconv.Atoi(ranges[1])
				if err != nil {
					panic(err)
				}
				to = to - 1
			} else {
				to = decryptedBodyLen - 1
			}
			decryptedBody = decryptedBody[from:to]
			resp.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from+1, to+1, decryptedBodyLen))
			resp.StatusCode = 206
			resp.Status = "Partial Content"
		}
		if req.Header.Get("If-Match") != "" &&
			req.Header.Get("If-Match") != etagString {
			resp.StatusCode = 416
			resp.Status = "Range Not Satisfiable"
			resp.Body = ioutil.NopCloser(strings.NewReader(""))
		} else {
			resp.Header.Set("Etag", etagString)
		}
		decryptedBodyLen = len(decryptedBody)
		//fmt.Println(decryptedBody)
		resp.Body = ioutil.NopCloser(bytes.NewReader(decryptedBody))
		resp.ContentLength = int64(decryptedBodyLen)
		resp.Header.Set("Content-Length", strconv.Itoa(decryptedBodyLen))
		//		resp.ContentLength = int64(decryptedBodyLen)
		//fmt.Println(hex.EncodeToString(hasher.Sum(nil)))

	}
	responseDump, err := httputil.DumpResponse(resp, false)
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println(string(responseDump))

	resp.Header.Set("X-Encrypting-Proxy", "yup")
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
