package main

import (
	"errors"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"

	"github.com/davecgh/go-spew/spew"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/protofire/proteus-shield/go-proxy-service-test/models"
	"gopkg.in/yaml.v3"
)

func isRequestMatchesRouteByHeaders(ctx *gin.Context, route models.Route, c chan bool) {
	matches := true
	for _, headerCatchConfig := range route.CatchConfig.Headers {
		re := regexp.MustCompile(headerCatchConfig.Value)
		currentHeaderValue := ctx.Request.Header.Get(headerCatchConfig.Name)
		if !re.Match([]byte(currentHeaderValue)) {
			matches = false
			break
		}
	}
	c <- matches
}

func isRequestMatchesRouteByQueryParams(ctx *gin.Context, route models.Route, c chan bool) {
	matches := true
	for _, paramCatchConfig := range route.CatchConfig.Params {
		re := regexp.MustCompile(paramCatchConfig.Value)
		currentParamValue := ctx.Query(paramCatchConfig.Name)
		if !re.Match([]byte(currentParamValue)) {
			matches = false
			break
		}
	}
	c <- matches
}

// Find the first route that matches the request
func findRoute(ctx *gin.Context, endpoint models.Endpoint) (*models.Route, error) {
	routeIndex := slices.IndexFunc(endpoint.Routes, func(route models.Route) bool {
		matchesRouteByHeadersChannel := make(chan bool)
		go isRequestMatchesRouteByHeaders(ctx, route, matchesRouteByHeadersChannel)

		matchesRouteByQueryParamsChannel := make(chan bool)
		go isRequestMatchesRouteByQueryParams(ctx, route, matchesRouteByQueryParamsChannel)

		return route.CatchConfig.Host == ctx.Request.Host && <-matchesRouteByHeadersChannel && <-matchesRouteByQueryParamsChannel
	})

	if routeIndex == -1 {
		return nil, errors.New("route not found")
	}

	return &endpoint.Routes[routeIndex], nil
}

func replaceRequestHeaderIfExists(ctx *gin.Context, header models.Header) {
	if currentValue := ctx.GetHeader(header.Name); currentValue != "" {
		ctx.Request.Header.Set(header.Name, header.Value)
	}
}

func addRequestHeaderIfNotExists(ctx *gin.Context, header models.Header) {
	if currentValue := ctx.GetHeader(header.Name); currentValue == "" {
		ctx.Request.Header.Add(header.Name, header.Value)
	}
}

// Put plugin logic in similar functions
func handleRequestTransformerPlugin(ctx *gin.Context, config models.RequestTransformerConfig) error {
	for _, header := range config.Replace.Headers {
		go replaceRequestHeaderIfExists(ctx, header)
	}

	for _, header := range config.Add.Headers {
		go addRequestHeaderIfNotExists(ctx, header)
	}

	return nil
}

func isWsRequest(ctx *gin.Context) bool {
	return ctx.GetHeader("Connection") == "Upgrade" && ctx.GetHeader("Upgrade") == "websocket"
}

type OnMessageCallback func([]byte)

func forwardMessages(in *websocket.Conn, out *websocket.Conn, callback OnMessageCallback) {
	// close all connections when the goroutine stops
	defer func() {
		in.Close()
		out.Close()
	}()

	for {
		messageType, message, err := in.ReadMessage()
		if err != nil {
			return
		}

		callback(message)

		if err := out.WriteMessage(messageType, message); err != nil {
			log.Printf("error writing message in loop %s -> %s: %v", in.LocalAddr(), out.LocalAddr(), err)
			return
		}
	}

}

func handleWs(ctx *gin.Context, host string, path string) {
	remoteUrl := "ws://" + host + path

	// dial upstream server and fail immediately if we can't
	upstreamConnection, _, err := websocket.DefaultDialer.Dial(remoteUrl, nil)
	if err != nil {
		log.Printf("failed to dial upstream: %v", err)
		return
	}

	// use custom upgrader to disable origin check
	connectionUpgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	// upgrade downstream connection and fail immediately if we can't
	downstremConnection, err := connectionUpgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		log.Printf("failed to upgrade downstream connection: %v", err)
		// close upstream connection if the downstream failed
		upstreamConnection.Close()
		return
	}

	// forward requests from the client to the upstream
	go forwardMessages(downstremConnection, upstreamConnection, func(b []byte) {
		log.Printf("got message from the client:")
	})

	// forward responses from the upstream to the client
	go forwardMessages(upstreamConnection, downstremConnection, func(b []byte) {
		log.Printf("got message from the upstream")
	})
}

func handleHttp(ctx *gin.Context, endpoint models.Endpoint) {
	route, err := findRoute(ctx, endpoint)
	if err != nil {
		ctx.AbortWithStatusJSON(500, gin.H{"error": err.Error()})
		return
	}

	remote, err := url.Parse("http://" + route.DestConfig.Host + ":" + strconv.FormatUint(route.DestConfig.Port, 10))
	if err != nil {
		ctx.AbortWithStatusJSON(500, gin.H{"error": err.Error()})
		return
	}

	// Put plugins chain here. The plugins are executed in order they appear in config
	for _, plugin := range route.Plugins {
		// We can disable plugins in config without removing them.
		// It's useful for testing and stuff
		if plugin.Disabled {
			continue
		}

		switch plugin.Type {
		case "request-transformer":
			if err := handleRequestTransformerPlugin(ctx, plugin.Config.(models.RequestTransformerConfig)); err != nil {
				ctx.AbortWithStatusJSON(500, gin.H{"error": err.Error()})
				return
			}
		}
	}

	destUrlPath := endpoint.Path + ctx.Param("proxyPath")
	// remove path prefix if path_mode is Prefix
	// and remove_path_prefix is enabled for the route
	if endpoint.PathMode == "Prefix" && route.DestConfig.RemovePathPrefix {
		destUrlPath = ctx.Param("proxyPath")
	}
	// replace path if dest.path is set
	if route.DestConfig.Path != "" {
		destUrlPath = route.DestConfig.Path
	}

	destMethod := ctx.Request.Method
	// replace method if dest.method is set
	if route.DestConfig.Method != "" {
		destMethod = route.DestConfig.Method
	}

	// run proxy depending on the protocol
	if isWsRequest(ctx) {
		handleWs(ctx, remote.Host, destUrlPath)
	} else {
		proxy := httputil.NewSingleHostReverseProxy(remote)
		proxy.Director = func(r *http.Request) {
			r.Method = destMethod
			r.Header = ctx.Request.Header
			r.Host = remote.Host
			r.URL.Scheme = remote.Scheme
			r.URL.Host = remote.Host
			r.URL.Path = destUrlPath
		}
		proxy.ServeHTTP(ctx.Writer, ctx.Request)
	}
}

// This is essentially a handler chooser
func proxy(endpoint models.Endpoint) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		handleHttp(ctx, endpoint)
	}
}

func loadRouterConfig(path string, config interface{}) error {
	file, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}

	if err := yaml.Unmarshal(file, config); err != nil {
		panic(err)
	}

	return nil
}

func main() {
	var routerConfig models.RouterConfig
	loadRouterConfig("./config.yaml", &routerConfig)
	spew.Dump((routerConfig))

	router := gin.Default()
	for _, endpoint := range routerConfig.Endpoints {
		// handle prefix endpoint paths
		endpointPath := endpoint.Path
		if endpoint.PathMode == "Prefix" {
			endpointPath = endpointPath + "/*proxyPath"
		}

		switch endpoint.Method {
		case "POST":
			router.POST(endpointPath, proxy(endpoint))
		case "GET":
			router.GET(endpointPath, proxy(endpoint))
		case "ANY":
			router.Any(endpointPath, proxy(endpoint))
		}
	}
	router.Run()
}
