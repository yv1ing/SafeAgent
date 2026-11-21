package console

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/swaggest/jsonschema-go"
	"github.com/swaggest/openapi-go"
	"github.com/swaggest/openapi-go/openapi3"
)

const contractDescription = "Failures return {code, message} with a 4xx or 5xx status. Requests that change state must send X-SafeAgent-Request: 1."

func serveSpecification(ctx *gin.Context) {
	payload, err := Specification()
	if err != nil {
		ctx.Status(http.StatusInternalServerError)
		return
	}
	ctx.Data(http.StatusOK, "application/json", payload)
}

// Specification reflects the registered endpoints. It needs no services, so the command line can
// print the contract without a database.
func Specification() ([]byte, error) {
	console := New(Services{}, Options{})
	reflector := openapi3.Reflector{}
	reflector.DefaultOptions = append(reflector.DefaultOptions, jsonschema.InterceptNullability(func(params jsonschema.InterceptNullabilityParams) {
		if params.Type.Kind() == reflect.Slice || params.Type.Kind() == reflect.Map {
			params.Schema.RemoveType(jsonschema.Null)
		}
	}))
	specification := reflector.SpecEns()
	specification.Info.Title = "SafeAgent console API"
	specification.Info.Version = "0.2.0"
	description := contractDescription
	specification.Info.Description = &description
	for _, entry := range console.endpoints {
		operation, err := reflector.NewOperationContext(entry.method, apiPrefix+entry.path)
		if err != nil {
			return nil, err
		}
		operation.SetID(entry.operation)
		operation.SetTags(tag(entry.path))
		if entry.input != nil {
			operation.AddReqStructure(entry.input, openapi.WithCustomize(func(content openapi.ContentOrReference) {
				if body, ok := content.(*openapi3.RequestBodyOrRef); ok {
					required := true
					body.RequestBody.Required = &required
				}
			}))
		}
		if entry.output != nil {
			operation.AddRespStructure(entry.output)
		} else {
			operation.AddRespStructure(nil, openapi.WithHTTPStatus(http.StatusNoContent))
		}
		operation.AddRespStructure(APIError{}, func(response *openapi.ContentUnit) { response.IsDefault = true })
		if !entry.anonymous {
			operation.AddSecurity("session")
		}
		if err := reflector.AddOperation(operation); err != nil {
			return nil, err
		}
	}
	specification.SetAPIKeySecurity("session", sessionCookie, openapi.InCookie, "Console session")
	encoded, err := json.Marshal(specification)
	if err != nil {
		return nil, err
	}
	var contract any
	if err := json.Unmarshal(encoded, &contract); err != nil {
		return nil, err
	}
	return json.MarshalIndent(contract, "", "  ")
}

// tag groups operations by the resource they act on: the first fixed path segment, skipping the
// "projects/{project_id}" prefix that scopes most resources.
func tag(path string) string {
	var fixed []string
	for _, segment := range strings.Split(strings.Trim(path, "/"), "/") {
		if !strings.HasPrefix(segment, "{") {
			fixed = append(fixed, segment)
		}
	}
	if len(fixed) > 1 && fixed[0] == "projects" {
		return fixed[1]
	}
	return fixed[0]
}
