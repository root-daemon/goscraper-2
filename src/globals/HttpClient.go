package globals

import "github.com/valyala/fasthttp"

var HttpClient = &fasthttp.Client{
	ReadBufferSize:  16384,
	WriteBufferSize: 4096,
}
