package handler

import (
	"github.com/graniticio/granitic/logging"
	"github.com/graniticio/granitic/ws"
	"net/http"
	"regexp"
	"github.com/graniticio/granitic/iam"
	"github.com/graniticio/granitic/httpendpoint"
)


// Indicates that an object is able to continue the processing of a web service request after the automated phases of
// parsing, binding, authenticating, authorising and auto-validating have been completed.
type WsRequestProcessor interface {
	Process(request *ws.WsRequest, response *ws.WsResponse)
}

// Indicates that an object is interested in observing/modifying a web service request after processing has been completed,
// but before the HTTP response is written. Typical uses are the writing of response headers that are generic to all/most handlers or the recording of metrics.
//
// It is expected that WsPostProcessors may be shared between multiple instances of WsHandler
type WsPostProcessor interface {
	PostProcess(handlerName string, request *ws.WsRequest, response *ws.WsResponse)
}


// Indicates that an object is interested in observing/modifying a web service request after it has been unmarshalled and parsed, but before automatic and
// application-defined validation takes place. If an error is encountered, or if the object decides that processing should be halted, it is expected that
// the implementing object adds one or more errors to the ws.WsResponse and returns false.
type WsPreValidateManipulator interface{
	PreValidate(request *ws.WsRequest, errors *ws.ServiceErrors) (proceed bool)
}

type WsRequestValidator interface {
	Validate(errors *ws.ServiceErrors, request *ws.WsRequest)
}

type WsUnmarshallTarget interface {
	UnmarshallTarget() interface{}
}

// Indicates that an object can determine whether or a handler supports a given version of a request.
type WsVersionAssessor interface {
	SupportsVersion(handlerName string, version httpendpoint.RequiredVersion) bool
}

//  WsHandler co-ordinates the processing of a web service request for a particular endpoint.
// Implements ws.HttpEndpointProvider
type WsHandler struct {
	AccessChecker         ws.WsAccessChecker          //
	AllowDirectHTTPAccess bool                        // Whether or not the underlying HTTP request and response writer should be made available to request Logic.
	AutoBindQuery         bool                        // Whether or not query parameters should be automatically injected into the request body.
	BindPathParams        []string                    // A list of fields on the request body that should be populated using elements of the request path.
	CheckAccessAfterParse bool                        // Check caller's permissions after request has been parsed (true) or before parsing (false).
	DeferFrameworkErrors  bool                        // If true, do not automatically return an error response if errors are found during the automated phases of request processing.
	DisableQueryParsing   bool                        // If true, discard the request's query parameters.
	DisablePathParsing    bool                        // If true, discard any path parameters found by match the request URI against the PathMatchPattern regex.
	ErrorFinder           ws.ServiceErrorFinder       // An object that provides access to application defined error messages for use during validation.
	FieldQueryParam       map[string]string           // A map of fields on the request body object and the names of query parameters that should be used to populate them
	FrameworkErrors       *ws.FrameworkErrorGenerator // An object that provides access to built-in error messages to use when an error is found during the automated phases of request processing.
	HttpMethod            string                      // The HTTP method (GET, POST etc) that this handler supports.
	Log                   logging.Logger              //
	Logic                 WsRequestProcessor          // The object representing the 'logic' behind this handler.
	ParamBinder           *ws.ParamBinder             //
	PathMatchPattern      string                      // A regex that will be matched against inbound request paths to check if this handler should be used to service the request.
	PostProcessor         WsPostProcessor             //
	PreValidateManipulator WsPreValidateManipulator	  //
	ResponseWriter        ws.WsResponseWriter         //
	RequireAuthentication bool                        // Whether on not the caller needs to be authenticated (using a ws.WsIdentifier) in order to access the logic behind this handler.
	Unmarshaller          ws.WsUnmarshaller           //
	UserIdentifier        ws.WsIdentifier             //
	VersionAssessor 	  WsVersionAssessor		   	  //
	bindPathParams        bool
	bindQuery             bool
	httpMethods           []string
	componentName         string
	pathRegex             *regexp.Regexp
	validate              bool
	validator             WsRequestValidator
}

func (wh *WsHandler) ProvideErrorFinder(finder ws.ServiceErrorFinder) {
	wh.ErrorFinder = finder
}

//HttpEndpointProvider
func (wh *WsHandler) ServeHTTP(w *httpendpoint.HTTPResponseWriter, req *http.Request) iam.ClientIdentity {

	defer func() {
		if r := recover(); r != nil {
			wh.Log.LogErrorfWithTrace("Panic recovered while trying process a request or write its response %s", r)
			wh.writePanicResponse(r, w)
		}
	}()

	wsReq := new(ws.WsRequest)
	wsReq.HttpMethod = req.Method

	if wh.AllowDirectHTTPAccess {
		da := new(ws.DirectHTTPAccess)
		da.Request = req
		da.ResponseWriter = w

		wsReq.UnderlyingHTTP = da
	}


	//Try to identify and/or authenticate the caller
	if !wh.identifyAndAuthenticate(w, req, wsReq) {
		return wsReq.UserIdentity
	}

	//Check caller has permission to use this resource
	if !wh.CheckAccessAfterParse && !wh.checkAccess(w, wsReq) {
		return wsReq.UserIdentity
	}

	//Unmarshall body, query parameters and path parameters
	wh.unmarshall(req, wsReq)
	wh.processQueryParams(req, wsReq)
	wh.processPathParams(req, wsReq)

	if wsReq.HasFrameworkErrors() && !wh.DeferFrameworkErrors {
		wh.handleFrameworkErrors(w, wsReq)
		return wsReq.UserIdentity
	}

	//Check caller has permission to use this resource
	if wh.CheckAccessAfterParse && !wh.checkAccess(w, wsReq) {
		return wsReq.UserIdentity
	}

	//Validate request
	var errors ws.ServiceErrors
	errors.ErrorFinder = wh.ErrorFinder

	if wh.validate {
		proceed := true

		if wh.PreValidateManipulator != nil {
			proceed = wh.PreValidateManipulator.PreValidate(wsReq, &errors)
		}

		if proceed {
			wh.validator.Validate(&errors, wsReq)
		}
	}

	if errors.HasErrors() {
		wh.writeErrorResponse(&errors, w)

		return wsReq.UserIdentity
	}

	//Execute logic
	wh.process(wsReq, w)

	return wsReq.UserIdentity
}

func (wh *WsHandler) unmarshall(req *http.Request, wsReq *ws.WsRequest) {

	targetSource, found := wh.Logic.(WsUnmarshallTarget)

	if found {
		target := targetSource.UnmarshallTarget()
		wsReq.RequestBody = target

		if req.ContentLength == 0 {
			return
		}

		err := wh.Unmarshaller.Unmarshall(req, wsReq)

		if err != nil {

			wh.Log.LogDebugf("Error unmarshalling request body for %s %s %s", req.URL.Path, req.Method, err)

			m, c := wh.FrameworkErrors.MessageCode(ws.UnableToParseRequest)

			f := ws.NewUnmarshallWsFrameworkError(m, c)
			wsReq.AddFrameworkError(f)
		}

	}
}

func (wh *WsHandler) processPathParams(req *http.Request, wsReq *ws.WsRequest) {

	if wh.DisablePathParsing {
		return
	}

	re := wh.pathRegex
	params := re.FindStringSubmatch(req.URL.Path)
	wsReq.PathParams = params[1:]

	if wh.bindPathParams && len(wsReq.PathParams) > 0 {
		pp := ws.NewWsParamsForPath(wh.BindPathParams, wsReq.PathParams)
		wh.ParamBinder.AutoBindPathParameters(wsReq, pp)
	}

}

func (wh *WsHandler) processQueryParams(req *http.Request, wsReq *ws.WsRequest) {

	if wh.DisableQueryParsing {
		return
	}

	values := req.URL.Query()
	wsReq.QueryParams = ws.NewWsParamsForQuery(values)

	if wh.bindQuery {
		if wsReq.RequestBody == nil {
			wh.Log.LogErrorf("Query parameter binding is enabled, but no target available to bind into. Does your Logic component implement the WsUnmarshallTarget interface?")
			return
		}

		if wh.AutoBindQuery {
			wh.ParamBinder.AutoBindQueryParameters(wsReq)
		} else {
			wh.ParamBinder.BindQueryParameters(wsReq, wh.FieldQueryParam)
		}

	}

}

func (wh *WsHandler) checkAccess(w *httpendpoint.HTTPResponseWriter, wsReq *ws.WsRequest) bool {

	ac := wh.AccessChecker

	if ac == nil {
		return true
	}

	allowed := ac.Allowed(wsReq)

	if allowed {
		return true
	} else {
		wh.ResponseWriter.WriteAbnormalStatus(http.StatusForbidden, w)
		return false
	}
}

func (wh *WsHandler) identifyAndAuthenticate(w *httpendpoint.HTTPResponseWriter, req *http.Request, wsReq *ws.WsRequest) bool {

	if wh.UserIdentifier != nil {
		i := wh.UserIdentifier.Identify(req)
		wsReq.UserIdentity = i

		if wh.RequireAuthentication && !i.Authenticated() {
			wh.ResponseWriter.WriteAbnormalStatus(http.StatusUnauthorized, w)
			return false
		}

	}

	if wsReq.UserIdentity == nil {
		wsReq.UserIdentity = iam.NewAnonymousIdentity()
	}

	return true

}

//HttpEndpointProvider
func (wh *WsHandler) SupportedHttpMethods() []string {
	if len(wh.httpMethods) > 0 {
		return wh.httpMethods
	} else {
		return []string{wh.HttpMethod}
	}
}

//HttpEndpointProvider
func (wh *WsHandler) RegexPattern() string {
	return wh.PathMatchPattern
}

//HttpEndpointProvider
func (wh *WsHandler) VersionAware() bool{
	return wh.VersionAssessor != nil
}

//HttpEndpointProvider
func (wh *WsHandler) SupportsVersion(version httpendpoint.RequiredVersion) bool{
	return wh.VersionAssessor.SupportsVersion(wh.ComponentName(), version)
}

func (wh *WsHandler) handleFrameworkErrors(w *httpendpoint.HTTPResponseWriter, wsReq *ws.WsRequest) {

	var se ws.ServiceErrors
	se.HttpStatus = http.StatusBadRequest

	for _, fe := range wsReq.FrameworkErrors {
		se.AddNewError(ws.Client, fe.Code, fe.Message)
	}

	wh.writeErrorResponse(&se, w)

}

func (wh *WsHandler) process(request *ws.WsRequest, w *httpendpoint.HTTPResponseWriter) {

	defer func() {
		if r := recover(); r != nil {
			wh.Log.LogErrorfWithTrace("Panic recovered while trying process a request or write its response %s", r)
			wh.writePanicResponse(r, w)
		}
	}()

	wsRes := ws.NewWsResponse(wh.ErrorFinder)
	wh.Logic.Process(request, wsRes)


	if wh.PostProcessor != nil {
		wh.PostProcessor.PostProcess(wh.ComponentName(), request, wsRes)
	}


	wh.ResponseWriter.Write(wsRes, w)

}

func (wh *WsHandler) writeErrorResponse(errors *ws.ServiceErrors, w *httpendpoint.HTTPResponseWriter) {

	l := wh.Log

	defer func() {
		if r := recover(); r != nil {
			l.LogErrorfWithTrace("Panic recovered while trying to write a response that was already in error %s", r)
		}
	}()

	err := wh.ResponseWriter.WriteErrors(errors, w)

	if err != nil {
		l.LogErrorf("Problem writing an HTTP response that was already in error", err)
	}

}

func (wh *WsHandler) writePanicResponse(r interface{}, w *httpendpoint.HTTPResponseWriter) {

	wh.ResponseWriter.WriteAbnormalStatus(http.StatusInternalServerError, w)

	wh.Log.LogErrorf("Panic recovered but error response served. %s", r)

}

func (wh *WsHandler) StartComponent() error {

	validator, found := wh.Logic.(WsRequestValidator)

	wh.validate = found

	if found {
		wh.validator = validator
	}

	wh.bindQuery = wh.AutoBindQuery || (wh.FieldQueryParam != nil && len(wh.FieldQueryParam) > 0)

	if !wh.DisablePathParsing {

		wh.bindPathParams = len(wh.BindPathParams) > 0

		r, err := regexp.Compile(wh.PathMatchPattern)

		if err != nil {
			return err
		} else {
			wh.pathRegex = r
		}

	}

	return nil

}

func (wh *WsHandler) ComponentName() string {
	return wh.componentName
}

func (wh *WsHandler) SetComponentName(name string) {
	wh.componentName = name
}
