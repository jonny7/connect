// Copyright 2024 Redpanda Data, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cassandra

import (
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"

	"github.com/redpanda-data/benthos/v4/public/service"
)

const (
	cFieldAddresses                               = "addresses"
	cFieldTLS                                     = "tls"
	cFieldPassAuth                                = "password_authenticator"
	cFieldPassAuthEnabled                         = "enabled"
	cFieldPassAuthUsername                        = "username"
	cFieldPassAuthPassword                        = "password"
	cFieldDisableIHL                              = "disable_initial_host_lookup"
	cFieldMaxRetries                              = "max_retries"
	cFieldBackoff                                 = "backoff"
	cFieldBackoffInitInterval                     = "initial_interval"
	cFieldBackoffMaxInterval                      = "max_interval"
	cFieldTimeout                                 = "timeout"
	cFieldWriteTimeout                            = "write_timeout"
	cFieldMaxRequestsPerConnection                = "max_requests_per_connection"
	cFieldReconnectInterval                       = "reconnect_interval"
	cFieldHostSelectionPolicy                     = "host_selection_policy"
	cFieldHostSelectionPolicyPrimary              = "primary"
	cFieldHostSelectionPolicyFallback             = "fallback"
	cFieldHostSelectionPolicyLocalDC              = "local_dc"
	cFieldHostSelectionPolicyLocalRack            = "local_rack"
	cFieldExponentialReconnectionPolicy           = "exponential_reconnection"
	cFieldExponentialReconnectionPolicyMaxRetries = "reconnection_max_retries"
	cFieldExponentialReconnectionInitialInterval  = "reconnection_initial_interval"
	cFieldExponentialReconnectionMaxInterval      = "reconnection_max_interval"
)

func clientFields() []*service.ConfigField {
	return []*service.ConfigField{
		service.NewStringListField(cFieldAddresses).
			Description("A list of Cassandra nodes to connect to. Multiple comma separated addresses can be specified on a single line.").
			Examples(
				[]string{"localhost:9042"},
				[]string{"foo:9042", "bar:9042"},
				[]string{"foo:9042,bar:9042"},
			),
		service.NewTLSToggledField(cFieldTLS).Advanced(),
		service.NewObjectField(cFieldPassAuth,
			service.NewBoolField(cFieldPassAuthEnabled).
				Description("Whether to use password authentication").
				Default(false),
			service.NewStringField(cFieldPassAuthUsername).
				Description("The username to authenticate as.").
				Default(""),
			service.NewStringField(cFieldPassAuthPassword).
				Description("The password to authenticate with.").
				Secret().
				Default(""),
		).
			Description("Optional configuration of Cassandra authentication parameters.").
			Advanced(),
		service.NewBoolField(cFieldDisableIHL).
			Description("If enabled the driver will not attempt to get host info from the system.peers table. This can speed up queries but will mean that data_centre, rack and token information will not be available.").
			Advanced().
			Default(false),
		service.NewIntField(cFieldMaxRetries).
			Description("The maximum number of retries before giving up on a request.").
			Advanced().
			Default(3),
		service.NewObjectField(cFieldBackoff,
			service.NewDurationField(cFieldBackoffInitInterval).
				Description("The initial period to wait between retry attempts.").
				Default("1s"),
			service.NewDurationField(cFieldBackoffMaxInterval).
				Description("The maximum period to wait between retry attempts.").
				Default("5s"),
		).
			Description("Control time intervals between retry attempts.").
			Advanced(),
		service.NewDurationField(cFieldTimeout).
			Description("The client connection timeout.").
			Default("600ms"),
		service.NewObjectField(cFieldHostSelectionPolicy,
			service.NewStringEnumField(cFieldHostSelectionPolicyPrimary, "round_robin", "token_aware").
				Description("host selection policy to use").
				Default("round_robin"),
			service.NewStringEnumField(cFieldHostSelectionPolicyFallback, "", "round_robin", "dc_aware", "rack_aware").
				Description("Optional fallback host selection policy to use").
				Default(""),
			service.NewStringField(cFieldHostSelectionPolicyLocalDC).
				Description("The local DC to use, this is only applicable for the DC Aware & Rack Aware policies").
				Default(""),
			service.NewStringField(cFieldHostSelectionPolicyLocalRack).
				Description("The local Rack to use, this is only applicable for the Rack Aware Policy").
				Optional(),
		).
			Description("Optional host selection policy configurations").
			Advanced(),
		service.NewDurationField(cFieldWriteTimeout).
			Description("limits the time the driver waits to write a request to a network connection").
			Default("600ms"),
		service.NewIntField(cFieldMaxRequestsPerConnection).
			Description("Maximum number of inflight requests allowed per connection").
			Default(32768),
		service.NewDurationField(cFieldReconnectInterval).
			Description("If not zero, gocql attempt to reconnect known DOWN nodes in every ReconnectInterval.").
			Default("60s"),
		service.NewObjectField(cFieldExponentialReconnectionPolicy,
			service.NewIntField(cFieldExponentialReconnectionPolicyMaxRetries).
				Description("The maximum number of retry attempts."),
			service.NewDurationField(cFieldExponentialReconnectionInitialInterval).
				Description("The initial period to wait between retry attempts."),
			service.NewDurationField(cFieldExponentialReconnectionMaxInterval).
				Description("The maximum period to wait between retry attempts."),
		).
			Description("Optional exponential reconnection policy, defaults to driver default of constant reconnection policy").
			Optional().
			Advanced(),
	}
}

type clientConf struct {
	addresses                []string
	tlsEnabled               bool
	tlsConf                  *tls.Config
	authEnabled              bool
	authUsername             string
	authPassword             string
	disableIHL               bool
	maxRetries               int
	backoffInitInterval      time.Duration
	backoffMaxInterval       time.Duration
	timeout                  time.Duration
	hostSelectionPolicy      gocql.HostSelectionPolicy
	writeTimeout             time.Duration
	maxRequestsPerConnection int
	reconnectInterval        time.Duration
	reconnectionPolicy       gocql.ReconnectionPolicy
}

func (c *clientConf) Create() (*gocql.ClusterConfig, error) {
	cluster := gocql.NewCluster(c.addresses...)
	if c.tlsEnabled {
		cluster.SslOpts = &gocql.SslOptions{
			Config: c.tlsConf,
		}
		cluster.DisableInitialHostLookup = c.tlsConf.InsecureSkipVerify
	} else {
		cluster.DisableInitialHostLookup = c.disableIHL
	}

	if c.authEnabled {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: c.authUsername,
			Password: c.authPassword,
		}
	}

	cluster.PoolConfig.HostSelectionPolicy = c.hostSelectionPolicy

	cluster.RetryPolicy = &decorator{
		NumRetries: c.maxRetries,
		Min:        c.backoffInitInterval,
		Max:        c.backoffMaxInterval,
	}

	cluster.Timeout = c.timeout
	cluster.WriteTimeout = c.writeTimeout
	cluster.MaxRequestsPerConn = c.maxRequestsPerConnection
	cluster.ReconnectInterval = c.reconnectInterval
	cluster.ReconnectionPolicy = c.reconnectionPolicy

	return cluster, nil
}

func clientConfFromParsed(conf *service.ParsedConfig) (c clientConf, err error) {
	var tmpAddresses []string
	if tmpAddresses, err = conf.FieldStringList(cFieldAddresses); err != nil {
		return
	}
	for _, a := range tmpAddresses {
		c.addresses = append(c.addresses, strings.Split(a, ",")...)
	}

	if c.tlsConf, c.tlsEnabled, err = conf.FieldTLSToggled(cFieldTLS); err != nil {
		return
	}

	{
		authConf := conf.Namespace(cFieldPassAuth)
		c.authEnabled, _ = authConf.FieldBool(cFieldPassAuthEnabled)
		c.authUsername, _ = authConf.FieldString(cFieldPassAuthUsername)
		c.authPassword, _ = authConf.FieldString(cFieldPassAuthPassword)
	}

	if c.disableIHL, err = conf.FieldBool(cFieldDisableIHL); err != nil {
		return
	}
	if c.maxRetries, err = conf.FieldInt(cFieldMaxRetries); err != nil {
		return
	}
	if c.backoffInitInterval, err = conf.FieldDuration(cFieldBackoff, cFieldBackoffInitInterval); err != nil {
		return
	}
	if c.backoffMaxInterval, err = conf.FieldDuration(cFieldBackoff, cFieldBackoffMaxInterval); err != nil {
		return
	}
	if c.timeout, err = conf.FieldDuration(cFieldTimeout); err != nil {
		return
	}
	if c.writeTimeout, err = conf.FieldDuration(cFieldWriteTimeout); err != nil {
		return
	}
	if c.maxRequestsPerConnection, err = conf.FieldInt(cFieldMaxRequestsPerConnection); err != nil {
		return
	}
	if c.reconnectInterval, err = conf.FieldDuration(cFieldReconnectInterval); err != nil {
		return
	}

	{
		hostSelection := conf.Namespace(cFieldHostSelectionPolicy)
		primary, _ := hostSelection.FieldString(cFieldHostSelectionPolicyPrimary)
		fallback, _ := hostSelection.FieldString(cFieldHostSelectionPolicyFallback)
		localDC, _ := hostSelection.FieldString(cFieldHostSelectionPolicyLocalDC)
		localRack, _ := hostSelection.FieldString(cFieldHostSelectionPolicyLocalRack)
		if c.hostSelectionPolicy, err = newHostSelectionPolicy(primaryHostSelection(primary), fallbackHostSelection(fallback), localDC, localRack); err != nil {
			return
		}
	}

	{
		reconnectionPolicy := conf.Namespace(cFieldExponentialReconnectionPolicy)
		initial, _ := reconnectionPolicy.FieldDuration(cFieldExponentialReconnectionInitialInterval)
		maxRetries, _ := reconnectionPolicy.FieldInt(cFieldExponentialReconnectionPolicyMaxRetries)
		maxInterval, _ := reconnectionPolicy.FieldDuration(cFieldExponentialReconnectionMaxInterval)
		c.reconnectionPolicy = newReconnectionPolicy(initial, maxRetries, maxInterval)
	}
	return
}

func newReconnectionPolicy(initialInterval time.Duration, MaxRetries int, MaxInterval time.Duration) gocql.ReconnectionPolicy {
	if initialInterval == 0 || MaxRetries == 0 || MaxInterval == 0 {
		return &gocql.ConstantReconnectionPolicy{MaxRetries: 3, Interval: 1 * time.Second}
	}
	return &gocql.ExponentialReconnectionPolicy{
		MaxRetries:      MaxRetries,
		InitialInterval: initialInterval,
		MaxInterval:     MaxInterval,
	}
}

type primaryHostSelection string

const (
	roundRobinPrimaryHostSelection primaryHostSelection = "round_robin"
	tokenAwarePrimaryHostSelection primaryHostSelection = "token_aware"
)

type fallbackHostSelection string

const (
	roundRobin fallbackHostSelection = "round_robin"
	rackAware  fallbackHostSelection = "rack_aware"
	dcAware    fallbackHostSelection = "dc_aware"
)

func newHostSelectionPolicy(policy primaryHostSelection, fallback fallbackHostSelection, localDC, localRack string) (gocql.HostSelectionPolicy, error) {
	switch policy {
	case roundRobinPrimaryHostSelection:
		return gocql.RoundRobinHostPolicy(), nil
	case tokenAwarePrimaryHostSelection:
		selectionPolicy, err := hostSelectionPolicy(fallback, localDC, localRack)
		if err != nil {
			return nil, fmt.Errorf("unable to create token-aware policy with fallback: %w", err)
		}
		return gocql.TokenAwareHostPolicy(selectionPolicy), nil
	default:
		return nil, fmt.Errorf("unsupported host selection policy: %s", policy)
	}
}

func hostSelectionPolicy(policy fallbackHostSelection, localDC, localRack string) (gocql.HostSelectionPolicy, error) {
	switch policy {
	case rackAware:
		if localDC != "" && localRack != "" {
			return gocql.RackAwareRoundRobinPolicy(localDC, localRack), nil
		}
		return nil, fmt.Errorf("rack-aware drivers require both a local DC and a local Rack")
	case dcAware:
		if localDC != "" {
			return gocql.DCAwareRoundRobinPolicy(localDC), nil
		}
		return nil, fmt.Errorf("dc-aware drivers require a local DC")
	case roundRobin:
		return gocql.RoundRobinHostPolicy(), nil
	default:
		return nil, fmt.Errorf("unknown primary host selection policy")
	}
}
