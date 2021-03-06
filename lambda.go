package awslambda

import (
	"net/http"

	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/mholt/caddy/caddyhttp/httpserver"
)

// Invoker calls a single AWS Lambda function - can be mocked for tests
type Invoker interface {
	Invoke(input *lambda.InvokeInput) (*lambda.InvokeOutput, error)
}

// Handler represents a middleware instance that can gateway requests to AWS Lambda
type Handler struct {
	Next    httpserver.Handler
	Configs []*Config
}

// ServeHTTP satisfies the httpserver.Handler interface by proxying
// the request to AWS Lambda via the Invoke function
//
// See: http://docs.aws.amazon.com/lambda/latest/dg/API_Invoke.html
//
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) (int, error) {
	conf, invokeInput, err := h.match(r)
	if err != nil {
		return 0, err
	}
	if conf == nil || conf.Path == "" || invokeInput == nil {
		return h.Next.ServeHTTP(w, r)
	}

	// Invoke function at AWS
	invokeOut, err := conf.invoker.Invoke(invokeInput)
	if err != nil {
		return 0, err
	}

	// Unpack the reply JSON
	reply, err := ParseReply(invokeOut.Payload)
	if err != nil {
		return 0, err
	}

	// Write the response HTTP headers
	for k, vals := range reply.Meta.Headers {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}

	// Default the Content-Type to application/json if not provided on reply
	if w.Header().Get("content-type") == "" {
		w.Header().Set("content-type", "application/json")
	}
	if reply.Meta.Status <= 0 {
		reply.Meta.Status = http.StatusOK
	}

	w.WriteHeader(reply.Meta.Status)

	// Write the response body
	_, err = w.Write([]byte(reply.Body))
	if err != nil {
		return 0, err
	}

	return reply.Meta.Status, nil
}

// match finds the best match for a proxy config based on r.
func (h Handler) match(r *http.Request) (*Config, *lambda.InvokeInput, error) {
	var c *Config
	var invokeInput *lambda.InvokeInput
	var err error
	var longestMatch int
	for _, conf := range h.Configs {
		basePath := conf.Path
		if httpserver.Path(r.URL.Path).Matches(basePath) && len(basePath) > longestMatch {
			// Convert the request to Invoke input struct
			invokeInput, err = conf.MaybeToInvokeInput(r)
			if err != nil {
				return c, nil, err
			} else if invokeInput != nil {
				longestMatch = len(basePath)
				c = conf
			}
		}
	}
	return c, invokeInput, nil
}
