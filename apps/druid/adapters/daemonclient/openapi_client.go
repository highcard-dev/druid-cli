package daemonclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/utils"
)

const (
	daemonDialTimeout    = 5 * time.Second
	daemonRequestTimeout = 30 * time.Minute
)

type OpenAPIClient struct {
	client     *api.ClientWithResponses
	server     string
	httpClient *http.Client
}

func NewOpenAPIClient(daemonSocket string) (*OpenAPIClient, error) {
	return NewOpenAPIClientForTarget(daemonSocket, "")
}

func NewOpenAPIClientForTarget(daemonSocket string, daemonURL string) (*OpenAPIClient, error) {
	if daemonURL != "" {
		server := strings.TrimRight(daemonURL, "/")
		httpClient := &http.Client{Timeout: daemonRequestTimeout}
		client, err := api.NewClientWithResponses(server, api.WithHTTPClient(httpClient))
		if err != nil {
			return nil, err
		}
		return &OpenAPIClient{client: client, server: server, httpClient: httpClient}, nil
	}
	if daemonSocket == "" {
		daemonSocket = utils.DefaultRuntimeSocketPath()
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network string, addr string) (net.Conn, error) {
			return (&net.Dialer{Timeout: daemonDialTimeout}).DialContext(ctx, "unix", daemonSocket)
		},
	}
	httpClient := &http.Client{Transport: transport, Timeout: daemonRequestTimeout}
	client, err := api.NewClientWithResponses("http://druid", api.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	return &OpenAPIClient{client: client, server: "http://druid", httpClient: httpClient}, nil
}

func (c *OpenAPIClient) CreateScroll(ctx context.Context, name string, artifact string, registryCredentials []api.RegistryCredential) (*api.RuntimeScroll, error) {
	var requestName *string
	if name != "" {
		requestName = &name
	}
	request := api.CreateScrollJSONRequestBody{
		Artifact: artifact,
		Name:     requestName,
	}
	if len(registryCredentials) > 0 {
		request.RegistryCredentials = &registryCredentials
	}
	res, err := c.client.CreateScrollWithResponse(ctx, request)
	if err != nil {
		return nil, err
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

func (c *OpenAPIClient) EnableWatch(ctx context.Context, id string, request api.DevWatchRequest) (*api.DevWatchResponse, error) {
	var out api.DevWatchResponse
	return &out, c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/v1/scrolls/%s/dev/enable", url.PathEscape(id)), request, &out)
}

func (c *OpenAPIClient) DisableWatch(ctx context.Context, id string) (*api.DevWatchResponse, error) {
	var out api.DevWatchResponse
	return &out, c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/v1/scrolls/%s/dev/disable", url.PathEscape(id)), nil, &out)
}

func (c *OpenAPIClient) WatchStatus(ctx context.Context, id string) (*api.DevWatchStatus, error) {
	var out api.DevWatchStatus
	return &out, c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/scrolls/%s/dev/status", url.PathEscape(id)), nil, &out)
}

func (c *OpenAPIClient) doJSON(ctx context.Context, method string, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.server+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := ensureStatus(resp.StatusCode, data); err != nil {
		return err
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

func ensureStatus(statusCode int, body []byte) error {
	if statusCode < 400 {
		return nil
	}
	return fmt.Errorf("daemon returned %d: %s", statusCode, strings.TrimSpace(string(body)))
}
