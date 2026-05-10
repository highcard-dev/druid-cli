package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/utils"
)

var ErrMaterializationUnsupported = errors.New("daemon materialization unsupported")

type OpenAPIClient struct {
	client *api.ClientWithResponses
}

func NewOpenAPIClient(daemonSocket string) (*OpenAPIClient, error) {
	if daemonSocket == "" {
		daemonSocket = utils.DefaultRuntimeSocketPath()
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network string, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", daemonSocket)
		},
	}
	client, err := api.NewClientWithResponses("http://druid", api.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		return nil, err
	}
	return &OpenAPIClient{client: client}, nil
}

func (c *OpenAPIClient) CreateScroll(ctx context.Context, name string, artifact string, scrollRoot string, dataRoot string, start bool) (*api.RuntimeScroll, error) {
	var requestName *string
	if name != "" {
		requestName = &name
	}
	var requestScrollRoot *string
	if scrollRoot != "" {
		requestScrollRoot = &scrollRoot
	}
	var requestDataRoot *string
	if dataRoot != "" {
		requestDataRoot = &dataRoot
	}
	res, err := c.client.CreateScrollWithResponse(ctx, api.CreateScrollJSONRequestBody{
		Artifact:   artifact,
		Name:       requestName,
		ScrollRoot: requestScrollRoot,
		DataRoot:   requestDataRoot,
		Start:      &start,
	})
	if err != nil {
		return nil, err
	}
	if res.StatusCode() == http.StatusNotImplemented {
		return nil, ErrMaterializationUnsupported
	}
	if err := ensureStatus(res.StatusCode(), res.Body); err != nil {
		return nil, err
	}
	return res.JSON201, nil
}

func (c *OpenAPIClient) ListScrolls(ctx context.Context) ([]api.RuntimeScroll, error) {
	res, err := c.client.ListScrollsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if err := ensureStatus(res.StatusCode(), res.Body); err != nil {
		return nil, err
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	return *res.JSON200, nil
}

func (c *OpenAPIClient) GetScroll(ctx context.Context, id string) (*api.RuntimeScroll, error) {
	res, err := c.client.GetScrollWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureStatus(res.StatusCode(), res.Body); err != nil {
		return nil, err
	}
	return res.JSON200, nil
}

func (c *OpenAPIClient) DeleteScroll(ctx context.Context, id string) (*api.DeletedScroll, error) {
	res, err := c.client.DeleteScrollWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureStatus(res.StatusCode(), res.Body); err != nil {
		return nil, err
	}
	return res.JSON200, nil
}

func (c *OpenAPIClient) RunScrollCommand(ctx context.Context, id string, command string) (*api.RuntimeScroll, error) {
	res, err := c.client.RunScrollCommandWithResponse(ctx, id, command)
	if err != nil {
		return nil, err
	}
	if err := ensureStatus(res.StatusCode(), res.Body); err != nil {
		return nil, err
	}
	return res.JSON200, nil
}

func (c *OpenAPIClient) GetScrollPorts(ctx context.Context, id string) ([]api.RuntimePortStatus, error) {
	res, err := c.client.GetScrollPortsWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureStatus(res.StatusCode(), res.Body); err != nil {
		return nil, err
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	return *res.JSON200, nil
}

func (c *OpenAPIClient) StartScroll(ctx context.Context, id string) (*api.RuntimeScroll, error) {
	res, err := c.client.StartScrollWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureStatus(res.StatusCode(), res.Body); err != nil {
		return nil, err
	}
	return res.JSON200, nil
}

func (c *OpenAPIClient) StopScroll(ctx context.Context, id string) (*api.RuntimeScroll, error) {
	res, err := c.client.StopScrollWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureStatus(res.StatusCode(), res.Body); err != nil {
		return nil, err
	}
	return res.JSON200, nil
}

func (c *OpenAPIClient) GetScrollRoutingTargets(ctx context.Context, id string) ([]api.RuntimeRoutingTarget, error) {
	res, err := c.client.GetScrollRoutingTargetsWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureStatus(res.StatusCode(), res.Body); err != nil {
		return nil, err
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	return *res.JSON200, nil
}

func (c *OpenAPIClient) ApplyScrollRouting(ctx context.Context, id string, assignments []api.RuntimeRouteAssignment) (*api.RuntimeScroll, error) {
	res, err := c.client.ApplyScrollRoutingWithResponse(ctx, id, api.ApplyRoutingRequest{Assignments: assignments})
	if err != nil {
		return nil, err
	}
	if err := ensureStatus(res.StatusCode(), res.Body); err != nil {
		return nil, err
	}
	return res.JSON200, nil
}

func ensureStatus(statusCode int, body []byte) error {
	if statusCode < 400 {
		return nil
	}
	return fmt.Errorf("daemon returned %d: %s", statusCode, strings.TrimSpace(string(body)))
}
