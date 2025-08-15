package models

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"strconv"
	"strings"

	"github.com/wangluozhe/chttp"
	"github.com/wangluozhe/chttp/cookiejar"
	"github.com/wangluozhe/requests/url"
	"github.com/wangluozhe/requests/utils"
)

// HTTP的所有请求方法
var MethodNames = []string{http.MethodGet, http.MethodPost, http.MethodOptions, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodTrace}

// 是否为HTTP请求方法
func inMethod(method string) bool {
	for _, value := range MethodNames {
		if value == method {
			return true
		}
	}
	return false
}

func NewPrepareRequest() *PrepareRequest {
	return &PrepareRequest{}
}

// PrepareRequest结构体
type PrepareRequest struct {
	Method  string
	Url     string
	Headers *http.Header
	Cookies *cookiejar.Jar
	Body    io.Reader
}

// 预处理所有数据
func (pr *PrepareRequest) Prepare(method, url string, params *url.Params, headers *http.Header, cookies *cookiejar.Jar, data *url.Values, files *url.Files, json map[string]interface{}, body io.Reader, auth []string) error {
	if err := pr.Prepare_method(method); err != nil {
		return err
	}
	if err := pr.Prepare_url(url, params); err != nil {
		return err
	}
	if err := pr.Prepare_headers(headers); err != nil {
		return err
	}
	pr.Prepare_cookies(cookies)
	if err := pr.Prepare_body(data, files, json, body); err != nil {
		return err
	}
	if err := pr.Prepare_auth(auth, url); err != nil {
		return err
	}
	return nil
}

// 预处理method
func (pr *PrepareRequest) Prepare_method(method string) error {
	method = strings.ToUpper(method)
	if !inMethod(method) {
		return errors.New("Method does not conform to HTTP protocol!")
	}
	pr.Method = method
	return nil
}

// 预处理url
func (pr *PrepareRequest) Prepare_url(rawurl string, params *url.Params) error {
	rawurl = strings.TrimSpace(rawurl)
	urls, err := url.Parse(rawurl)
	if err != nil {
		return err
	}
	if urls.Scheme == "" {
		return fmt.Errorf("Invalid URL %s: No scheme supplied. Perhaps you meant http://%s?", rawurl, rawurl)
	} else if urls.Host == "" {
		return fmt.Errorf("Invalid URL %s: No host supplied", rawurl)
	}
	if urls.Path == "" {
		urls.Path = "/"
	}
	if params != nil {
		if urls.RawParams != "" {
			urls.Params = url.ParseParams(urls.RawParams + "&" + params.Encode())
		} else {
			urls.Params = url.ParseParams(params.Encode())
		}
	}
	pr.Url = urls.String()
	return nil
}

// 预处理headers
func (pr *PrepareRequest) Prepare_headers(headers *http.Header) error {
	pr.Headers = url.NewHeaders()
	if headers != nil {
		pr.Headers = headers
	}
	return nil
}

// 预处理body
func (pr *PrepareRequest) Prepare_body(data *url.Values, files *url.Files, json map[string]interface{}, bodys io.Reader) error {
	if bodys != nil {
		if pr.Headers.Get("content-type") == "" {
			pr.Headers.Set("content-type", "application/octet-stream")
		}
		pr.Body = bodys
		return nil
	}

	var body string
	var contentType string
	var err error

	if data == nil && json != nil {
		contentType = "application/json"
		body, err = prepareJSONBody(json)
		if err != nil {
			return err
		}
	}
	if files != nil {
		body, contentType, err = prepareFilesBody(files, data)
		if err != nil {
			return err
		}
	} else if data != nil {
		contentType = "application/x-www-form-urlencoded"
		body = data.Encode()
	}
	//pr.prepare_content_length(body) // 禁用预处理body大小，防止dll无法正常访问
	if contentType != "" && pr.Headers.Get("Content-Type") == "" {
		pr.Headers.Set("Content-Type", contentType)
	}
	pr.Body = strings.NewReader(body)
	return nil
}

// 预处理JSONBody
func prepareJSONBody(json map[string]interface{}) (string, error) {
	jsonByte, err := utils.Marshal(json) // 避免json.Marshal对 "<", ">", "&" 等字符进行HTML编码
	if err != nil {
		return "", err
	}
	return string(jsonByte), nil
}

// 预处理FilesBody
func prepareFilesBody(files *url.Files, data *url.Values) (string, string, error) {
	var byteBuffer *bytes.Buffer
	var contentType string
	var err error

	if data != nil {
		for _, key := range data.Keys() {
			files.AddField(key, data.Get(key))
		}
	}
	byteBuffer, contentType, err = files.Encode()
	if err != nil {
		return "", "", err
	}

	bodyByte, err := io.ReadAll(byteBuffer)
	if err != nil {
		return "", "", err
	}
	return string(bodyByte), contentType, nil
}

// 预处理body大小
func (pr *PrepareRequest) prepare_content_length(body string) {
	if body != "" {
		length := len(body)
		if length > 0 {
			pr.Headers.Set("Content-Length", strconv.Itoa(length))
		}
	} else if (pr.Method != "GET" || pr.Method != "HEAD") && pr.Headers.Get("Content-Length") == "" {
		pr.Headers.Set("Content-Length", "0")
	}
}

// 预处理cookie
func (pr *PrepareRequest) Prepare_cookies(cookies *cookiejar.Jar) {
	if cookies != nil {
		pr.Cookies = cookies
	} else {
		pr.Cookies, _ = cookiejar.New(nil)
	}
}

// 预处理auth
func (pr *PrepareRequest) Prepare_auth(auth []string, rawurl string) error {
	if auth == nil {
		urls, err := url.Parse(utils.EncodeURI(rawurl))
		if err != nil {
			return err
		}
		user := urls.User.String()
		if user != "" {
			auth = append(auth, user)
		}
		pass, _ := urls.User.Password()
		if pass != "" {
			auth = append(auth, pass)
		}
	}
	if auth != nil && len(auth) == 2 {
		pr.Headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(strings.Join(auth, ":"))))
	}
	return nil
}

func (pr *PrepareRequest) Hash() string {
	bytes, err := json.Marshal(pr)
	if err != nil {
		return ""
	}

	h := fnv.New64a()
	h.Write(bytes)
	return strconv.Itoa(int(h.Sum64()))
}
