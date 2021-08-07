// Copyright (c) 2017 jelmersnoeck
// Copyright (c) 2018,2019 Aiven, Helsinki, Finland. https://aiven.io/

package aiven

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	retryhttp "github.com/hashicorp/go-retryablehttp"
)

// Client represents the instance that does all the calls to the Aiven API.
type Client struct {
	apiKey    string
	apiUrl    string
	userAgent string
	client    *retryhttp.Client

	Projects                        *ProjectsHandler
	ProjectUsers                    *ProjectUsersHandler
	CA                              *CAHandler
	CardsHandler                    *CardsHandler
	ServiceIntegrationEndpoints     *ServiceIntegrationEndpointsHandler
	ServiceIntegrations             *ServiceIntegrationsHandler
	Services                        *ServicesHandler
	ConnectionPools                 *ConnectionPoolsHandler
	Databases                       *DatabasesHandler
	ServiceUsers                    *ServiceUsersHandler
	KafkaACLs                       *KafkaACLHandler
	KafkaSubjectSchemas             *KafkaSubjectSchemasHandler
	KafkaGlobalSchemaConfig         *KafkaGlobalSchemaConfigHandler
	KafkaConnectors                 *KafkaConnectorsHandler
	KafkaMirrorMakerReplicationFlow *MirrorMakerReplicationFlowHandler
	ElasticsearchACLs               *ElasticSearchACLsHandler
	KafkaTopics                     *KafkaTopicsHandler
	VPCs                            *VPCsHandler
	VPCPeeringConnections           *VPCPeeringConnectionsHandler
	Accounts                        *AccountsHandler
	AccountTeams                    *AccountTeamsHandler
	AccountTeamMembers              *AccountTeamMembersHandler
	AccountTeamProjects             *AccountTeamProjectsHandler
	AccountAuthentications          *AccountAuthenticationsHandler
	AccountTeamInvites              *AccountTeamInvitesHandler
	TransitGatewayVPCAttachment     *TransitGatewayVPCAttachmentHandler
	BillingGroup                    *BillingGroupHandler
	ServiceTask                     *ServiceTaskHandler
	AWSPrivatelink                  *AWSPrivatelinkHandler
}

type Option func(*clientParameters)

func WithHTTPClient(c *http.Client) Option {
	return func(cp *clientParameters) {
		cp.httpClient = c
	}
}

func WithAPIUrl(url string) Option {
	return func(cp *clientParameters) {
		cp.apiUrl = url
	}
}

func WithTokenAuth(token string) Option {
	return func(cp *clientParameters) {
		cp.authMethod = tokenAuth{token}
	}
}

func WithMFAAuth(email, password, otp string) Option {
	return func(cp *clientParameters) {
		cp.authMethod = mfaAuth{email: email, otp: otp, password: password}
	}
}

func WithUserAuth(email, password string) Option {
	return func(cp *clientParameters) {
		cp.authMethod = mfaAuth{email: email, password: password}
	}
}

func WithUserAgent(userAgent string) Option {
	return func(cp *clientParameters) {
		cp.userAgent = userAgent
	}
}

func WithRetries(retryCount uint, retryBackoff time.Duration) Option {
	return func(cp *clientParameters) {
		cp.retryCount = retryCount
		cp.retryBackoff = retryBackoff
	}
}

type clientParameters struct {
	httpClient *http.Client
	apiUrl     string
	userAgent  string
	authMethod authMethod

	retryCount   uint
	retryBackoff time.Duration
}

type authMethod interface {
	// token provides the API token that authorizes the client.
	// takes a *Client parameter because it may use the API.
	token(*Client) (string, error)
}

type mfaAuth struct {
	email, otp, password string
}

func (mfa mfaAuth) token(c *Client) (string, error) {
	bts, err := c.doPostRequest("/userauth", authRequest{mfa.email, mfa.otp, mfa.password})
	if err != nil {
		return "", fmt.Errorf("unable to perform /userauth request: %w", err)
	}

	var r authResponse
	if err := checkAPIResponse(bts, &r); err != nil {
		return "", fmt.Errorf("bad API response: %w", err)
	}
	return r.Token, nil
}

type tokenAuth struct {
	apiToken string
}

func (ta tokenAuth) token(*Client) (string, error) {
	return ta.apiToken, nil
}

func defaultClientParameters() clientParameters {
	return clientParameters{
		httpClient:   http.DefaultClient,
		apiUrl:       "https://api.aiven.io",
		userAgent:    "aiven-go-client/" + Version(),
		retryCount:   2,
		retryBackoff: 1 * time.Second,
	}
}

// NewClientWithOptions creates a new client. Configuration is performed via options
func NewClientWithOptions(opts ...Option) (*Client, error) {
	clientParameters := defaultClientParameters()
	for i := range opts {
		opts[i](&clientParameters)
	}

	if clientParameters.authMethod == nil {
		return nil, fmt.Errorf("must provide authorization method")
	}

	delegate := retryhttp.NewClient()
	delegate.HTTPClient = clientParameters.httpClient
	delegate.RetryMax = int(clientParameters.retryCount)
	delegate.RetryWaitMin = clientParameters.retryBackoff
	delegate.RetryWaitMax = clientParameters.retryBackoff

	c := &Client{
		client: delegate,
		apiUrl: clientParameters.apiUrl,
	}

	// the client still needs to be authorized
	token, err := clientParameters.authMethod.token(c)
	if err != nil {
		return nil, fmt.Errorf("unable to authorize client: %w", err)
	}
	c.apiKey = token

	c.Projects = &ProjectsHandler{c}
	c.ProjectUsers = &ProjectUsersHandler{c}
	c.CA = &CAHandler{c}
	c.CardsHandler = &CardsHandler{c}
	c.ServiceIntegrationEndpoints = &ServiceIntegrationEndpointsHandler{c}
	c.ServiceIntegrations = &ServiceIntegrationsHandler{c}
	c.Services = &ServicesHandler{c}
	c.ConnectionPools = &ConnectionPoolsHandler{c}
	c.Databases = &DatabasesHandler{c}
	c.ServiceUsers = &ServiceUsersHandler{c}
	c.KafkaACLs = &KafkaACLHandler{c}
	c.KafkaSubjectSchemas = &KafkaSubjectSchemasHandler{c}
	c.KafkaGlobalSchemaConfig = &KafkaGlobalSchemaConfigHandler{c}
	c.KafkaConnectors = &KafkaConnectorsHandler{c}
	c.KafkaMirrorMakerReplicationFlow = &MirrorMakerReplicationFlowHandler{c}
	c.ElasticsearchACLs = &ElasticSearchACLsHandler{c}
	c.KafkaTopics = &KafkaTopicsHandler{c}
	c.VPCs = &VPCsHandler{c}
	c.VPCPeeringConnections = &VPCPeeringConnectionsHandler{c}
	c.Accounts = &AccountsHandler{c}
	c.AccountTeams = &AccountTeamsHandler{c}
	c.AccountTeamMembers = &AccountTeamMembersHandler{c}
	c.AccountTeamProjects = &AccountTeamProjectsHandler{c}
	c.AccountAuthentications = &AccountAuthenticationsHandler{c}
	c.AccountTeamInvites = &AccountTeamInvitesHandler{c}
	c.TransitGatewayVPCAttachment = &TransitGatewayVPCAttachmentHandler{c}
	c.BillingGroup = &BillingGroupHandler{c}
	c.ServiceTask = &ServiceTaskHandler{c}
	c.AWSPrivatelink = &AWSPrivatelinkHandler{c}

	return c, nil
}

// NewMFAUserClient creates a new client based on email, one-time password and password.
// Deprecated: use NewClientWithOptions
func NewMFAUserClient(email, otp, password string, userAgent string) (*Client, error) {
	return NewClientWithOptions(
		WithHTTPClient(buildHttpClient()),
		WithUserAgent(GetUserAgentOrDefault(userAgent)),
		WithMFAAuth(email, password, otp),
		WithAPIUrl(GetApiUrlFromEnvOrDefault()),
	)
}

// NewUserClient creates a new client based on email and password.
// Deprecated: use NewClientWithOptions
func NewUserClient(email, password string, userAgent string) (*Client, error) {
	return NewClientWithOptions(
		WithHTTPClient(buildHttpClient()),
		WithUserAgent(GetUserAgentOrDefault(userAgent)),
		WithUserAuth(email, password),
		WithAPIUrl(GetApiUrlFromEnvOrDefault()),
	)
}

// NewTokenClient creates a new client based on a given token.
// Deprecated: use NewClientWithOptions
func NewTokenClient(key string, userAgent string) (*Client, error) {
	return NewClientWithOptions(
		WithHTTPClient(buildHttpClient()),
		WithUserAgent(GetUserAgentOrDefault(userAgent)),
		WithTokenAuth(key),
		WithAPIUrl(GetApiUrlFromEnvOrDefault()),
	)
}

// GetUserAgentOrDefault configures a default userAgent value, if one has not been provided.
// Deprecated: just pass the user agent using the option "WithUserAgent"
// needed for backwards compatibility
func GetUserAgentOrDefault(userAgent string) string {
	if userAgent != "" {
		return userAgent
	}
	return "aiven-go-client/" + Version()
}

// Deprecated: just pass the api url using the option "WithAPIUrl"
// needed for backwards compatibility
func GetApiUrlFromEnvOrDefault() string {
	if value, ok := os.LookupEnv("AIVEN_WEB_URL"); !ok {
		return "https://api.aiven.io"
	} else {
		return value
	}
}

// buildHttpClient it builds http.Client, if environment variable AIVEN_CA_CERT
// contains a path to a valid CA certificate HTTPS client will be configured to use it
// Deprecated: just pass an appropriate *http.Client using the option "WithHTTPClient"
// needed for backwards compatibility
func buildHttpClient() *http.Client {
	caFilename := os.Getenv("AIVEN_CA_CERT")
	if caFilename == "" {
		return &http.Client{}
	}

	// Load CA cert
	caCert, err := ioutil.ReadFile(caFilename)
	if err != nil {
		log.Fatal("cannot load ca cert: %w", err)
	}

	// Append CA cert to the system pool
	caCertPool, _ := x509.SystemCertPool()
	if caCertPool == nil {
		caCertPool = x509.NewCertPool()
	}

	if ok := caCertPool.AppendCertsFromPEM(caCert); !ok {
		log.Println("[WARNING] No certs appended, using system certs only")
	}

	// Setup HTTPS client
	tlsConfig := &tls.Config{
		RootCAs: caCertPool,
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	client := &http.Client{Transport: transport}

	return client
}

// TODO: these methods probably should return (*http.Response, error)
func (c *Client) doGetRequest(endpoint string, req interface{}) ([]byte, error) {
	return c.doRequest(http.MethodGet, endpoint, req, 1)
}

func (c *Client) doPutRequest(endpoint string, req interface{}) ([]byte, error) {
	return c.doRequest(http.MethodPut, endpoint, req, 1)
}

func (c *Client) doPostRequest(endpoint string, req interface{}) ([]byte, error) {
	return c.doRequest(http.MethodPost, endpoint, req, 1)
}

func (c *Client) doDeleteRequest(endpoint string, req interface{}) ([]byte, error) {
	return c.doRequest(http.MethodDelete, endpoint, req, 1)
}

func (c *Client) doV2GetRequest(endpoint string, req interface{}) ([]byte, error) {
	return c.doRequest(http.MethodGet, endpoint, req, 2)
}

func (c *Client) doV2PutRequest(endpoint string, req interface{}) ([]byte, error) {
	return c.doRequest(http.MethodPut, endpoint, req, 2)
}

func (c *Client) doV2PostRequest(endpoint string, req interface{}) ([]byte, error) {
	return c.doRequest(http.MethodPost, endpoint, req, 2)
}

func (c *Client) doV2DeleteRequest(endpoint string, req interface{}) ([]byte, error) {
	return c.doRequest(http.MethodDelete, endpoint, req, 2)
}

func (c *Client) doRequest(method, uri string, body interface{}, apiVersion int) (res []byte, err error) {
	var bts []byte
	if body != nil {
		if bts, err = json.Marshal(body); err != nil {
			return nil, fmt.Errorf("unable to marshal request body: %w", err)
		}
	}

	var url string
	switch apiVersion {
	case 1:
		url = c.endpoint(uri)
	case 2:
		url = c.endpointV2(uri)
	default:
		return nil, fmt.Errorf("aiven API apiVersion `%d` is not supported", apiVersion)
	}

	req, err := retryhttp.NewRequest(method, url, bytes.NewBuffer(bts))
	if err != nil {
		return nil, fmt.Errorf("unable to build http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Authorization", "aivenv1 "+c.apiKey)

	rsp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to perform http request: %w", err)
	}
	defer func() { _ = rsp.Body.Close() }()

	res, err = ioutil.ReadAll(rsp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read request body: %w", err)
	}
	switch sc := rsp.StatusCode; {
	case sc >= 200 && sc < 300:
		// 2xx
		return res, nil
	case sc >= 400 && sc < 600:
		// 4xx or 5xx
		/*
		   TODO: include the aiven error fields here, they look like
		   "errors": [
		     {
		       "message": "string",
		       "more_info": "string",
		       "status": 0
		     }
		   ],
		*/
		return nil, Error{Message: string(res), Status: sc}
	default:
		// 1xx or 3xx or weird
		return nil, Error{Message: fmt.Sprintf("unexpected status code, also: %s", res), Status: sc}
	}
}

func (c Client) endpoint(uri string) string {
	return c.apiUrl + "/v1" + uri
}

func (c Client) endpointV2(uri string) string {
	return c.apiUrl + "/v2" + uri
}

// ToStringPointer converts string to a string pointer
func ToStringPointer(s string) *string {
	return &s
}
