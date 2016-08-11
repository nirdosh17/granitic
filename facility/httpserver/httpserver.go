package httpserver

import (
	"errors"
	"fmt"
	"github.com/graniticio/granitic/ioc"
	"github.com/graniticio/granitic/logging"
	"github.com/graniticio/granitic/ws"
	"net/http"
	"regexp"
	"time"
)

type RegisteredProvider struct {
	Provider HttpEndpointProvider
	Pattern  *regexp.Regexp
}

type HttpServer struct {
	registeredProvidersByMethod map[string][]*RegisteredProvider
	componentContainer          *ioc.ComponentContainer
	FrameworkLogger             logging.Logger
	AccessLogWriter             *AccessLogWriter
	AccessLogging               bool
	Port                        int
	ContentType                 string
	Encoding                    string
	AbnormalStatusWriter        ws.AbnormalStatusWriter
	AbnormalStatusWriterName    string
	abnormalWriters             map[string]ws.AbnormalStatusWriter
}

func (h *HttpServer) Container(container *ioc.ComponentContainer) {
	h.componentContainer = container
}

func (h *HttpServer) registerProvider(endPointProvider HttpEndpointProvider) {

	for _, method := range endPointProvider.SupportedHttpMethods() {

		pattern := endPointProvider.RegexPattern()
		compiledRegex, regexError := regexp.Compile(pattern)

		if regexError != nil {
			h.FrameworkLogger.LogErrorf("Unable to compile regular expression from pattern %s: %s", pattern, regexError.Error())
		}

		h.FrameworkLogger.LogTracef("Registering %s %s", pattern, method)

		rp := RegisteredProvider{endPointProvider, compiledRegex}

		providersForMethod := h.registeredProvidersByMethod[method]

		if providersForMethod == nil {
			providersForMethod = make([]*RegisteredProvider, 1)
			providersForMethod[0] = &rp
			h.registeredProvidersByMethod[method] = providersForMethod
		} else {
			h.registeredProvidersByMethod[method] = append(providersForMethod, &rp)
		}
	}

}

func (h *HttpServer) StartComponent() error {

	h.registeredProvidersByMethod = make(map[string][]*RegisteredProvider)

	for name, component := range h.componentContainer.AllComponents() {
		provider, found := component.Instance.(HttpEndpointProvider)

		if found {
			h.FrameworkLogger.LogDebugf("Found HttpEndpointProvider %s", name)

			h.registerProvider(provider)

		}
	}

	if h.AbnormalStatusWriter == nil {

		m := h.abnormalWriters
		l := len(m)

		if l == 0 {
			return errors.New("No instance of ws.AbnormalStatusWriter available.")
		} else {

			if l > 2 && h.AbnormalStatusWriterName == "" {
				return errors.New("More than one instance of ws.AbnormalStatusWriter available, but AbnormalStatusWriterName is not set.")
			}

			for k, v := range m {

				if l == 1 {
					h.AbnormalStatusWriter = v
					break
				}

				if k == h.AbnormalStatusWriterName {
					h.AbnormalStatusWriter = v
					break
				}
			}

			if h.AbnormalStatusWriter == nil {
				message := fmt.Sprintf("None of the available ws.AbnormalStatusWriter instances available have the name %s", h.AbnormalStatusWriterName)
				return errors.New(message)
			}

		}

	}

	return nil
}

func (h *HttpServer) AllowAccess() error {
	http.Handle("/", http.HandlerFunc(h.handleAll))

	listenAddress := fmt.Sprintf(":%d", h.Port)

	go http.ListenAndServe(listenAddress, nil)

	h.FrameworkLogger.LogInfof("HTTP server started listening on %d", h.Port)

	return nil
}

func (h *HttpServer) handleAll(res http.ResponseWriter, req *http.Request) {

	received := time.Now()
	matched := false

	providersByMethod := h.registeredProvidersByMethod[req.Method]

	path := req.URL.Path

	h.FrameworkLogger.LogTracef("Finding provider to handle %s %s from %d providers", path, req.Method, len(providersByMethod))

	wrw := new(wrappedResponseWriter)
	wrw.rw = res

	var identity ws.WsIdentity

	for _, handlerPattern := range providersByMethod {

		pattern := handlerPattern.Pattern

		h.FrameworkLogger.LogTracef("Testing %s", pattern.String())

		if pattern.MatchString(path) {
			h.FrameworkLogger.LogTracef("Matches %s", pattern.String())
			matched = true
			identity = handlerPattern.Provider.ServeHTTP(wrw, req)
		}
	}

	if !matched {
		h.AbnormalStatusWriter.WriteAbnormalStatus(http.StatusNotFound, res)
	}

	if h.AccessLogging {
		finished := time.Now()
		h.AccessLogWriter.LogRequest(req, wrw, &received, &finished, identity)
	}

}

func (h *HttpServer) RegisterAbnormalStatusWriter(name string, w ws.AbnormalStatusWriter) {
	if h.abnormalWriters == nil {
		h.abnormalWriters = make(map[string]ws.AbnormalStatusWriter)
	}

	h.abnormalWriters[name] = w
}

type wrappedResponseWriter struct {
	rw          http.ResponseWriter
	Status      int
	BytesServed int
}

func (wrw *wrappedResponseWriter) Header() http.Header {
	return wrw.rw.Header()
}

func (wrw *wrappedResponseWriter) Write(b []byte) (int, error) {

	wrw.BytesServed += len(b)

	return wrw.rw.Write(b)
}

func (wrw *wrappedResponseWriter) WriteHeader(i int) {
	wrw.Status = i
	wrw.rw.WriteHeader(i)
}

type AbnormalStatusWriterDecorator struct {
	FrameworkLogger logging.Logger
	HttpServer      *HttpServer
}

func (d *AbnormalStatusWriterDecorator) OfInterest(component *ioc.Component) bool {

	i := component.Instance

	_, found := i.(ws.AbnormalStatusWriter)

	return found

}

func (d *AbnormalStatusWriterDecorator) DecorateComponent(component *ioc.Component, container *ioc.ComponentContainer) {

	i := component.Instance.(ws.AbnormalStatusWriter)

	d.HttpServer.RegisterAbnormalStatusWriter(component.Name, i)
}
