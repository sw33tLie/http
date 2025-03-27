// Code generated by golang.org/x/tools/cmd/bundle. DO NOT EDIT.
//go:generate bundle -prefix= -o=httpcommon.go golang.org/x/net/internal/httpcommon

package httpcommon

import (
	"context"
	"errors"
	"fmt"
	"github.com/sw33tLie/http/httptrace"
	"net/textproto"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/sw33tLie/http/internal/httpguts"
	"golang.org/x/net/http2/hpack"
)

// The HTTP protocols are defined in terms of ASCII, not Unicode. This file
// contains helper functions which may use Unicode-aware functions which would
// otherwise be unsafe and could introduce vulnerabilities if used improperly.

// asciiEqualFold is strings.EqualFold, ASCII only. It reports whether s and t
// are equal, ASCII-case-insensitively.
func asciiEqualFold(s, t string) bool {
	if len(s) != len(t) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if lower(s[i]) != lower(t[i]) {
			return false
		}
	}
	return true
}

// lower returns the ASCII lowercase version of b.
func lower(b byte) byte {
	if 'A' <= b && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// isASCIIPrint returns whether s is ASCII and printable according to
// https://tools.ietf.org/html/rfc20#section-4.2.
func isASCIIPrint(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < ' ' || s[i] > '~' {
			return false
		}
	}
	return true
}

// asciiToLower returns the lowercase version of s if s is ASCII and printable,
// and whether or not it was.
func asciiToLower(s string) (lower string, ok bool) {
	if !isASCIIPrint(s) {
		return "", false
	}
	return strings.ToLower(s), true
}

var (
	commonBuildOnce   sync.Once
	commonLowerHeader map[string]string // Go-Canonical-Case -> lower-case
	commonCanonHeader map[string]string // lower-case -> Go-Canonical-Case
)

func buildCommonHeaderMapsOnce() {
	commonBuildOnce.Do(buildCommonHeaderMaps)
}

func buildCommonHeaderMaps() {
	common := []string{
		"accept",
		"accept-charset",
		"accept-encoding",
		"accept-language",
		"accept-ranges",
		"age",
		"access-control-allow-credentials",
		"access-control-allow-headers",
		"access-control-allow-methods",
		"access-control-allow-origin",
		"access-control-expose-headers",
		"access-control-max-age",
		"access-control-request-headers",
		"access-control-request-method",
		"allow",
		"authorization",
		"cache-control",
		"content-disposition",
		"content-encoding",
		"content-language",
		"content-length",
		"content-location",
		"content-range",
		"content-type",
		"cookie",
		"date",
		"etag",
		"expect",
		"expires",
		"from",
		"host",
		"if-match",
		"if-modified-since",
		"if-none-match",
		"if-unmodified-since",
		"last-modified",
		"link",
		"location",
		"max-forwards",
		"origin",
		"proxy-authenticate",
		"proxy-authorization",
		"range",
		"referer",
		"refresh",
		"retry-after",
		"server",
		"set-cookie",
		"strict-transport-security",
		"trailer",
		"transfer-encoding",
		"user-agent",
		"vary",
		"via",
		"www-authenticate",
		"x-forwarded-for",
		"x-forwarded-proto",
	}
	commonLowerHeader = make(map[string]string, len(common))
	commonCanonHeader = make(map[string]string, len(common))
	for _, v := range common {
		chk := textproto.CanonicalMIMEHeaderKey(v)
		commonLowerHeader[chk] = v
		commonCanonHeader[v] = chk
	}
}

// LowerHeader returns the lowercase form of a header name,
// used on the wire for HTTP/2 and HTTP/3 requests.
func LowerHeader(v string) (lower string, ascii bool) {
	buildCommonHeaderMapsOnce()
	if s, ok := commonLowerHeader[v]; ok {
		return s, true
	}
	return asciiToLower(v)
}

// CanonicalHeader canonicalizes a header name. (For example, "host" becomes "Host".)
func CanonicalHeader(v string) string {
	buildCommonHeaderMapsOnce()
	if s, ok := commonCanonHeader[v]; ok {
		return s
	}
	return textproto.CanonicalMIMEHeaderKey(v)
}

// CachedCanonicalHeader returns the canonical form of a well-known header name.
func CachedCanonicalHeader(v string) (string, bool) {
	buildCommonHeaderMapsOnce()
	s, ok := commonCanonHeader[v]
	return s, ok
}

var (
	ErrRequestHeaderListSize = errors.New("request header list larger than peer's advertised limit")
)

// Request is a subset of http.Request.
// It'd be simpler to pass an *http.Request, of course, but we can't depend on net/http
// without creating a dependency cycle.
type Request struct {
	URL                 *url.URL
	Method              string
	Host                string
	Header              map[string][]string
	Trailer             map[string][]string
	ActualContentLength int64 // 0 means 0, -1 means unknown
}

// EncodeHeadersParam is parameters to EncodeHeaders.
type EncodeHeadersParam struct {
	Request Request

	// AddGzipHeader indicates that an "accept-encoding: gzip" header should be
	// added to the request.
	AddGzipHeader bool

	// PeerMaxHeaderListSize, when non-zero, is the peer's MAX_HEADER_LIST_SIZE setting.
	PeerMaxHeaderListSize uint64

	// DefaultUserAgent is the User-Agent header to send when the request
	// neither contains a User-Agent nor disables it.
	DefaultUserAgent string
}

// EncodeHeadersParam is the result of EncodeHeaders.
type EncodeHeadersResult struct {
	HasBody     bool
	HasTrailers bool
}

// EncodeHeaders constructs request headers common to HTTP/2 and HTTP/3.
// It validates a request and calls headerf with each pseudo-header and header
// for the request.
// The headerf function is called with the validated, canonicalized header name.
func EncodeHeaders(ctx context.Context, param EncodeHeadersParam, headerf func(name, value string)) (res EncodeHeadersResult, _ error) {
	req := param.Request

	// Check for invalid connection-level headers.
	if err := checkConnHeaders(req.Header); err != nil {
		return res, err
	}

	if req.URL == nil {
		return res, errors.New("Request.URL is nil")
	}

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	host, err := httpguts.PunycodeHostPort(host)
	if err != nil {
		return res, err
	}
	if !httpguts.ValidHostHeader(host) {
		return res, errors.New("invalid Host header")
	}

	// isNormalConnect is true if this is a non-extended CONNECT request.
	isNormalConnect := false
	var protocol string
	if vv := req.Header[":protocol"]; len(vv) > 0 {
		protocol = vv[0]
	}
	if req.Method == "CONNECT" && protocol == "" {
		isNormalConnect = true
	} else if protocol != "" && req.Method != "CONNECT" {
		return res, errors.New("invalid :protocol header in non-CONNECT request")
	}

	// Validate the path, except for non-extended CONNECT requests which have no path.
	var path string
	if !isNormalConnect {
		path = req.URL.RequestURI()
		if !validPseudoPath(path) {
			orig := path
			path = strings.TrimPrefix(path, req.URL.Scheme+"://"+host)
			if !validPseudoPath(path) {
				if req.URL.Opaque != "" {
					return res, fmt.Errorf("invalid request :path %q from URL.Opaque = %q", orig, req.URL.Opaque)
				} else {
					return res, fmt.Errorf("invalid request :path %q", orig)
				}
			}
		}
	}

	// Check for any invalid headers+trailers and return an error before we
	// potentially pollute our hpack state. (We want to be able to
	// continue to reuse the hpack encoder for future requests)
	if err := validateHeaders(req.Header); err != "" {
		return res, fmt.Errorf("invalid HTTP header %s", err)
	}
	if err := validateHeaders(req.Trailer); err != "" {
		return res, fmt.Errorf("invalid HTTP trailer %s", err)
	}

	trailers, err := commaSeparatedTrailers(req.Trailer)
	if err != nil {
		return res, err
	}

	enumerateHeaders := func(f func(name, value string)) {
		// 8.1.2.3 Request Pseudo-Header Fields
		// The :path pseudo-header field includes the path and query parts of the
		// target URI (the path-absolute production and optionally a '?' character
		// followed by the query production, see Sections 3.3 and 3.4 of
		// [RFC3986]).
		f(":authority", host)
		m := req.Method
		if m == "" {
			m = "GET"
		}
		f(":method", m)
		if !isNormalConnect {
			f(":path", path)
			f(":scheme", req.URL.Scheme)
		}
		if protocol != "" {
			f(":protocol", protocol)
		}
		if trailers != "" {
			f("trailer", trailers)
		}

		var didUA bool
		for k, vv := range req.Header {
			if asciiEqualFold(k, "host") || asciiEqualFold(k, "content-length") {
				// Host is :authority, already sent.
				// Content-Length is automatic, set below.
				continue
			} else if asciiEqualFold(k, "connection") ||
				asciiEqualFold(k, "proxy-connection") ||
				asciiEqualFold(k, "transfer-encoding") ||
				asciiEqualFold(k, "upgrade") ||
				asciiEqualFold(k, "keep-alive") {
				// Per 8.1.2.2 Connection-Specific Header
				// Fields, don't send connection-specific
				// fields. We have already checked if any
				// are error-worthy so just ignore the rest.
				continue
			} else if asciiEqualFold(k, "user-agent") {
				// Match Go's http1 behavior: at most one
				// User-Agent. If set to nil or empty string,
				// then omit it. Otherwise if not mentioned,
				// include the default (below).
				didUA = true
				if len(vv) < 1 {
					continue
				}
				vv = vv[:1]
				if vv[0] == "" {
					continue
				}
			} else if asciiEqualFold(k, "cookie") {
				// Per 8.1.2.5 To allow for better compression efficiency, the
				// Cookie header field MAY be split into separate header fields,
				// each with one or more cookie-pairs.
				for _, v := range vv {
					for {
						p := strings.IndexByte(v, ';')
						if p < 0 {
							break
						}
						f("cookie", v[:p])
						p++
						// strip space after semicolon if any.
						for p+1 <= len(v) && v[p] == ' ' {
							p++
						}
						v = v[p:]
					}
					if len(v) > 0 {
						f("cookie", v)
					}
				}
				continue
			} else if k == ":protocol" {
				// :protocol pseudo-header was already sent above.
				continue
			}

			for _, v := range vv {
				f(k, v)
			}
		}
		if shouldSendReqContentLength(req.Method, req.ActualContentLength) {
			f("content-length", strconv.FormatInt(req.ActualContentLength, 10))
		}
		if param.AddGzipHeader {
			f("accept-encoding", "gzip")
		}
		if !didUA {
			f("user-agent", param.DefaultUserAgent)
		}
	}

	// Do a first pass over the headers counting bytes to ensure
	// we don't exceed cc.peerMaxHeaderListSize. This is done as a
	// separate pass before encoding the headers to prevent
	// modifying the hpack state.
	if param.PeerMaxHeaderListSize > 0 {
		hlSize := uint64(0)
		enumerateHeaders(func(name, value string) {
			hf := hpack.HeaderField{Name: name, Value: value}
			hlSize += uint64(hf.Size())
		})

		if hlSize > param.PeerMaxHeaderListSize {
			return res, ErrRequestHeaderListSize
		}
	}

	trace := httptrace.ContextClientTrace(ctx)

	// Header list size is ok. Write the headers.
	enumerateHeaders(func(name, value string) {
		name, ascii := LowerHeader(name)
		if !ascii {
			// Skip writing invalid headers. Per RFC 7540, Section 8.1.2, header
			// field names have to be ASCII characters (just as in HTTP/1.x).
			return
		}

		headerf(name, value)

		if trace != nil && trace.WroteHeaderField != nil {
			trace.WroteHeaderField(name, []string{value})
		}
	})

	res.HasBody = req.ActualContentLength != 0
	res.HasTrailers = trailers != ""
	return res, nil
}

// IsRequestGzip reports whether we should add an Accept-Encoding: gzip header
// for a request.
func IsRequestGzip(method string, header map[string][]string, disableCompression bool) bool {
	// TODO(bradfitz): this is a copy of the logic in net/http. Unify somewhere?
	if !disableCompression &&
		len(header["Accept-Encoding"]) == 0 &&
		len(header["Range"]) == 0 &&
		method != "HEAD" {
		// Request gzip only, not deflate. Deflate is ambiguous and
		// not as universally supported anyway.
		// See: https://zlib.net/zlib_faq.html#faq39
		//
		// Note that we don't request this for HEAD requests,
		// due to a bug in nginx:
		//   http://trac.nginx.org/nginx/ticket/358
		//   https://golang.org/issue/5522
		//
		// We don't request gzip if the request is for a range, since
		// auto-decoding a portion of a gzipped document will just fail
		// anyway. See https://golang.org/issue/8923
		return true
	}
	return false
}

// checkConnHeaders checks whether req has any invalid connection-level headers.
//
// https://www.rfc-editor.org/rfc/rfc9114.html#section-4.2-3
// https://www.rfc-editor.org/rfc/rfc9113.html#section-8.2.2-1
//
// Certain headers are special-cased as okay but not transmitted later.
// For example, we allow "Transfer-Encoding: chunked", but drop the header when encoding.
func checkConnHeaders(h map[string][]string) error {
	if vv := h["Upgrade"]; len(vv) > 0 && (vv[0] != "" && vv[0] != "chunked") {
		return fmt.Errorf("invalid Upgrade request header: %q", vv)
	}
	if vv := h["Transfer-Encoding"]; len(vv) > 0 && (len(vv) > 1 || vv[0] != "" && vv[0] != "chunked") {
		return fmt.Errorf("invalid Transfer-Encoding request header: %q", vv)
	}
	if vv := h["Connection"]; len(vv) > 0 && (len(vv) > 1 || vv[0] != "" && !asciiEqualFold(vv[0], "close") && !asciiEqualFold(vv[0], "keep-alive")) {
		return fmt.Errorf("invalid Connection request header: %q", vv)
	}
	return nil
}

func commaSeparatedTrailers(trailer map[string][]string) (string, error) {
	keys := make([]string, 0, len(trailer))
	for k := range trailer {
		k = CanonicalHeader(k)
		switch k {
		case "Transfer-Encoding", "Trailer", "Content-Length":
			return "", fmt.Errorf("invalid Trailer key %q", k)
		}
		keys = append(keys, k)
	}
	if len(keys) > 0 {
		sort.Strings(keys)
		return strings.Join(keys, ","), nil
	}
	return "", nil
}

// validPseudoPath reports whether v is a valid :path pseudo-header
// value. It must be either:
//
//   - a non-empty string starting with '/'
//   - the string '*', for OPTIONS requests.
//
// For now this is only used a quick check for deciding when to clean
// up Opaque URLs before sending requests from the Transport.
// See golang.org/issue/16847
//
// We used to enforce that the path also didn't start with "//", but
// Google's GFE accepts such paths and Chrome sends them, so ignore
// that part of the spec. See golang.org/issue/19103.
func validPseudoPath(v string) bool {
	return (len(v) > 0 && v[0] == '/') || v == "*"
}

func validateHeaders(hdrs map[string][]string) string {
	for k, vv := range hdrs {
		if !httpguts.ValidHeaderFieldName(k) && k != ":protocol" {
			return fmt.Sprintf("name %q", k)
		}
		for _, v := range vv {
			if !httpguts.ValidHeaderFieldValue(v) {
				// Don't include the value in the error,
				// because it may be sensitive.
				return fmt.Sprintf("value for header %q", k)
			}
		}
	}
	return ""
}

// shouldSendReqContentLength reports whether we should send
// a "content-length" request header. This logic is basically a copy of the net/http
// transferWriter.shouldSendContentLength.
// The contentLength is the corrected contentLength (so 0 means actually 0, not unknown).
// -1 means unknown.
func shouldSendReqContentLength(method string, contentLength int64) bool {
	if contentLength > 0 {
		return true
	}
	if contentLength < 0 {
		return false
	}
	// For zero bodies, whether we send a content-length depends on the method.
	// It also kinda doesn't matter for http2 either way, with END_STREAM.
	switch method {
	case "POST", "PUT", "PATCH":
		return true
	default:
		return false
	}
}

// ServerRequestParam is parameters to NewServerRequest.
type ServerRequestParam struct {
	Method                  string
	Scheme, Authority, Path string
	Protocol                string
	Header                  map[string][]string
}

// ServerRequestResult is the result of NewServerRequest.
type ServerRequestResult struct {
	// Various http.Request fields.
	URL        *url.URL
	RequestURI string
	Trailer    map[string][]string

	NeedsContinue bool // client provided an "Expect: 100-continue" header

	// If the request should be rejected, this is a short string suitable for passing
	// to the http2 package's CountError function.
	// It might be a bit odd to return errors this way rather than returing an error,
	// but this ensures we don't forget to include a CountError reason.
	InvalidReason string
}

func NewServerRequest(rp ServerRequestParam) ServerRequestResult {
	needsContinue := httpguts.HeaderValuesContainsToken(rp.Header["Expect"], "100-continue")
	if needsContinue {
		delete(rp.Header, "Expect")
	}
	// Merge Cookie headers into one "; "-delimited value.
	if cookies := rp.Header["Cookie"]; len(cookies) > 1 {
		rp.Header["Cookie"] = []string{strings.Join(cookies, "; ")}
	}

	// Setup Trailers
	var trailer map[string][]string
	for _, v := range rp.Header["Trailer"] {
		for _, key := range strings.Split(v, ",") {
			key = textproto.CanonicalMIMEHeaderKey(textproto.TrimString(key))
			switch key {
			case "Transfer-Encoding", "Trailer", "Content-Length":
				// Bogus. (copy of http1 rules)
				// Ignore.
			default:
				if trailer == nil {
					trailer = make(map[string][]string)
				}
				trailer[key] = nil
			}
		}
	}
	delete(rp.Header, "Trailer")

	// "':authority' MUST NOT include the deprecated userinfo subcomponent
	// for "http" or "https" schemed URIs."
	// https://www.rfc-editor.org/rfc/rfc9113.html#section-8.3.1-2.3.8
	if strings.IndexByte(rp.Authority, '@') != -1 && (rp.Scheme == "http" || rp.Scheme == "https") {
		return ServerRequestResult{
			InvalidReason: "userinfo_in_authority",
		}
	}

	var url_ *url.URL
	var requestURI string
	if rp.Method == "CONNECT" && rp.Protocol == "" {
		url_ = &url.URL{Host: rp.Authority}
		requestURI = rp.Authority // mimic HTTP/1 server behavior
	} else {
		var err error
		url_, err = url.ParseRequestURI(rp.Path)
		if err != nil {
			return ServerRequestResult{
				InvalidReason: "bad_path",
			}
		}
		requestURI = rp.Path
	}

	return ServerRequestResult{
		URL:           url_,
		NeedsContinue: needsContinue,
		RequestURI:    requestURI,
		Trailer:       trailer,
	}
}
