/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package config

import (
	"encoding/json"
	"net"
	"strings"
	"time"

	"github.com/alipay/sofa-mosn/pkg/api/v2"
	"github.com/alipay/sofa-mosn/pkg/log"
	"github.com/alipay/sofa-mosn/pkg/protocol"
	"github.com/alipay/sofa-mosn/pkg/server"
)

type ContentKey string

var ProtocolsSupported = map[string]bool{
	string(protocol.SofaRPC):   true,
	string(protocol.HTTP2):     true,
	string(protocol.HTTP1):     true,
	string(protocol.Xprotocol): true,
}

// callback when corresponding module parsed
type ParsedCallback func(data interface{}, endParsing bool) error

// notes: configcontentkey equals to the key of config file
const (
	ParseCallbackKeyCluster        ContentKey = "clusters"
	ParseCallbackKeyServiceRgtInfo ContentKey = "service_registry"
)

func RegisterConfigParsedListener(key ContentKey, cb ParsedCallback) {
	if cbs, ok := configParsedCBMaps[key]; ok {
		cbs = append(cbs, cb)
	} else {
		log.StartLogger.Infof(" %s added to configParsedCBMaps", key)
		cpc := []ParsedCallback{cb}
		configParsedCBMaps[key] = cpc
	}
}

var (
	configParsedCBMaps = make(map[ContentKey][]ParsedCallback)

	logLevelMap = map[string]log.Level{
		"TRACE": log.TRACE,
		"DEBUG": log.DEBUG,
		"FATAL": log.FATAL,
		"ERROR": log.ERROR,
		"WARN":  log.WARN,
		"INFO":  log.INFO,
	}

	clusterTypeMap = map[string]v2.ClusterType{
		"SIMPLE":  v2.SIMPLE_CLUSTER,
		"DYNAMIC": v2.DYNAMIC_CLUSTER,
	}

	lbTypeMap = map[string]v2.LbType{
		"LB_RANDOM":     v2.LB_RANDOM,
		"LB_ROUNDROBIN": v2.LB_ROUNDROBIN,
	}
)

func ParseLogLevel(level string) log.Level {
	if level != "" {
		if logLevel, ok := logLevelMap[level]; ok {
			return logLevel
		}

		log.StartLogger.Fatalln("unsupported log level: ", level)
	}
	//use INFO as default log level
	return log.INFO
}

func ParseServerConfig(c *ServerConfig) *server.Config {
	sc := &server.Config{
		LogPath:         c.DefaultLogPath,
		LogLevel:        ParseLogLevel(c.DefaultLogLevel),
		GracefulTimeout: c.GracefulTimeout.Duration,
		Processor:       c.Processor,
	}

	return sc
}

func ParseProxyFilterJSON(c *v2.Filter) *v2.Proxy {

	proxyConfig := &v2.Proxy{}

	if data, err := json.Marshal(c.Config); err == nil {
		json.Unmarshal(data, &proxyConfig)
	} else {
		log.StartLogger.Fatal("Parsing Proxy Network Fitler Error")
	}

	if proxyConfig.DownstreamProtocol == "" || proxyConfig.UpstreamProtocol == "" {
		log.StartLogger.Fatal("Protocol in String Needed in Proxy Network Fitler")
	} else if _, ok := ProtocolsSupported[proxyConfig.DownstreamProtocol]; !ok {
		log.StartLogger.Fatal("Invalid Downstream Protocol = ", proxyConfig.DownstreamProtocol)
	} else if _, ok := ProtocolsSupported[proxyConfig.UpstreamProtocol]; !ok {
		log.StartLogger.Fatal("Invalid Upstream Protocol = ", proxyConfig.UpstreamProtocol)
	}

	if !proxyConfig.SupportDynamicRoute {
		log.StartLogger.Warnf("Mesh Doesn't Support Dynamic Router")
	}

	if len(proxyConfig.VirtualHosts) == 0 {
		log.StartLogger.Warnf("No VirtualHosts Founded")

	} else {

		for _, vh := range proxyConfig.VirtualHosts {

			if len(vh.Routers) == 0 {
				log.StartLogger.Warnf("No Router Founded in VirtualHosts")
			}
		}
	}

	proxyConfig.BasicRoutes = ParseBasicFilter(proxyConfig)

	return proxyConfig
}

func GetServiceFromHeader(router *v2.Router) *v2.BasicServiceRoute {

	if router == nil {
		return nil
	}

	var ServiceName, ClusterName string

	for _, h := range router.Match.Headers {
		if h.Name == "service" || h.Name == "Service" {
			ServiceName = h.Value
		}
	}

	ClusterName = router.Route.ClusterName

	if ServiceName == "" || ClusterName == "" {
		return nil
	}

	return &v2.BasicServiceRoute{
		Service: ServiceName,
		Cluster: ClusterName,
	}
}

func ParseBasicFilter(proxy *v2.Proxy) []*v2.BasicServiceRoute {

	var BSR []*v2.BasicServiceRoute

	for _, p := range proxy.VirtualHosts {

		for _, r := range p.Routers {
			BSR = append(BSR, GetServiceFromHeader(&r))
		}
	}
	return BSR
}

func ParseProxyFilter(c *v2.Filter) *v2.Proxy {
	proxyConfig := &v2.Proxy{}

	//downstream protocol
	//TODO config(json object) extract and type convert util
	if downstreamProtocol, ok := c.Config["downstream_protocol"]; ok {
		if downstreamProtocol, ok := downstreamProtocol.(string); ok {
			proxyConfig.DownstreamProtocol = downstreamProtocol
		} else {
			log.StartLogger.Fatalln("[downstream_protocol] in proxy filter config is not string")
		}
	} else {
		log.StartLogger.Fatalln("[downstream_protocol] is required in proxy filter config")
	}

	//upstream protocol
	if upstreamProtocol, ok := c.Config["upstream_protocol"]; ok {
		if upstreamProtocol, ok := upstreamProtocol.(string); ok {
			proxyConfig.UpstreamProtocol = upstreamProtocol
		} else {
			log.StartLogger.Fatalln("[upstream_protocol] in proxy filter config is not string")
		}
	} else {
		log.StartLogger.Fatalln("[upstream_protocol] is required in proxy filter config")
	}

	//todo support dynamic route or not, save
	if dynamicBool, ok := c.Config["support_dynamic_route"]; ok {
		if dynamicBool, ok := dynamicBool.(bool); ok {
			proxyConfig.SupportDynamicRoute = dynamicBool
		} else {
			log.StartLogger.Fatalln("support_dynamic_route in proxy filter support_dynamic_route is not bool")
		}
	} else {
		log.StartLogger.Debugf("support_dynamic_route doesn't set in proxy filter config")
	}

	//routes
	if routes, ok := c.Config["routes"]; ok {
		if routes, ok := routes.([]interface{}); ok {
			for _, route := range routes {
				proxyConfig.BasicRoutes = append(proxyConfig.BasicRoutes, parseRouteConfig(route.(map[string]interface{})))
			}
		} else {
			log.StartLogger.Fatalln("[routes] in proxy filter config is not list of routemap")
		}
	} else {
		log.StartLogger.Fatalln("[routes] is required in proxy filter config")
	}

	return proxyConfig
}

func ParseAccessConfig(c []AccessLogConfig) []v2.AccessLog {
	var logs []v2.AccessLog

	for _, logConfig := range c {
		logs = append(logs, v2.AccessLog{
			Path:   logConfig.LogPath,
			Format: logConfig.LogFormat,
		})
	}

	return logs
}

func ParseFilterChains(c []FilterChain) []v2.FilterChain {
	var filterchains []v2.FilterChain

	for _, fc := range c {
		filters := make([]v2.Filter, 0)
		for _, f := range fc.Filters {
			filters = append(filters, v2.Filter{
				Name:   f.Type,
				Config: f.Config,
			})
		}

		filterchains = append(filterchains, v2.FilterChain{
			FilterChainMatch: fc.FilterChainMatch,
			TLS:              ParseTLSConfig(&fc.TLS),
			Filters:          filters,
		})
	}

	return filterchains
}

func ParseTLSConfig(tlsconfig *TLSConfig) v2.TLSConfig {
	if tlsconfig.Status == false {
		return v2.TLSConfig{
			Status: false,
		}
	}

	if (tlsconfig.VerifyClient || tlsconfig.VerifyServer) && tlsconfig.CACert == "" {
		log.StartLogger.Fatalln("[CaCert] is required in TLS config")
	}

	return v2.TLSConfig{
		Status:       tlsconfig.Status,
		Inspector:    tlsconfig.Inspector,
		ServerName:   tlsconfig.ServerName,
		CACert:       tlsconfig.CACert,
		CertChain:    tlsconfig.CertChain,
		PrivateKey:   tlsconfig.PrivateKey,
		VerifyClient: tlsconfig.VerifyClient,
		VerifyServer: tlsconfig.VerifyServer,
		CipherSuites: tlsconfig.CipherSuites,
		EcdhCurves:   tlsconfig.EcdhCurves,
		MinVersion:   tlsconfig.MinVersion,
		MaxVersion:   tlsconfig.MaxVersion,
		ALPN:         tlsconfig.ALPN,
		Ticket:       tlsconfig.Ticket,
	}
}

func parseRouteConfig(config map[string]interface{}) *v2.BasicServiceRoute {
	route := &v2.BasicServiceRoute{}

	//name
	if name, ok := config["name"]; ok {
		if name, ok := name.(string); ok {
			route.Name = name
		} else {
			log.StartLogger.Fatalln("[name] in proxy filter route config is not string")
		}
	} else {
		log.StartLogger.Fatalln("[name] is required in proxy filter route config")
	}

	//service
	if service, ok := config["service"]; ok {
		if service, ok := service.(string); ok {
			route.Service = service
		} else {
			log.StartLogger.Fatalln("[service] in proxy filter route config is not string")
		}
	} else {
		log.StartLogger.Fatalln("[service] is required in proxy filter route config")
	}

	//cluster
	if cluster, ok := config["cluster"]; ok {
		if cluster, ok := cluster.(string); ok {
			route.Cluster = cluster
		} else {
			log.StartLogger.Fatalln("[cluster] in proxy filter route config is not string")
		}
	} else {
		log.StartLogger.Fatalln("[cluster] is required in proxy filter route config")
	}

	return route
}

func ParseFaultInjectFilter(config map[string]interface{}) *v2.FaultInject {

	faultInject := &v2.FaultInject{}

	//percent
	if percent, ok := config["delay_percent"]; ok {
		if percent, ok := percent.(float64); ok {
			faultInject.DelayPercent = uint32(percent)
		} else {
			log.StartLogger.Fatalln("[delay_percent] in fault inject filter config is not integer")
		}
	} else {
		log.StartLogger.Fatalln("[delay_percent] is required in fault inject filter config")
	}

	//duration
	if duration, ok := config["delay_duration"]; ok {
		if duration, ok := duration.(string); ok {
			if duration, error := time.ParseDuration(strings.Trim(duration, `"`)); error == nil {
				faultInject.DelayDuration = uint64(duration)
			} else {
				log.StartLogger.Fatalln("[delay_duration] in fault inject filter config is not valid ,", error)
			}
		} else {
			log.StartLogger.Fatalln("[delay_duration] in fault inject filter config is not a numeric string, like '30s'")
		}
	} else {
		log.StartLogger.Fatalln("[delay_duration] is required in fault inject filter config")
	}

	return faultInject
}

func ParseHealthcheckFilter(config map[string]interface{}) *v2.HealthCheckFilter {
	healthcheck := &v2.HealthCheckFilter{}

	//passthrough
	if passthrough, ok := config["passthrough"]; ok {
		if passthrough, ok := passthrough.(bool); ok {
			healthcheck.PassThrough = passthrough
		} else {
			log.StartLogger.Fatalln("[passthrough] in health check filter config is not bool")
		}
	} else {
		log.StartLogger.Fatalln("[passthrough] is required in healthcheck filter config")
	}

	//cache time
	if cacheTime, ok := config["cache_time"]; ok {
		if cacheTime, ok := cacheTime.(string); ok {
			if duration, error := time.ParseDuration(strings.Trim(cacheTime, `"`)); error == nil {
				healthcheck.CacheTime = duration
			} else {
				log.StartLogger.Fatalln("[cache_time] in health check filter is not valid ,", error)
			}
		} else {
			log.StartLogger.Fatalln("[cache_time] in health check filter config is not a numeric string")
		}
	} else {
		log.StartLogger.Fatalln("[cache_time] is required in healthcheck filter config")
	}

	//cluster_min_healthy_percentagesp
	if clusterMinHealthyPercentage, ok := config["cluster_min_healthy_percentages"]; ok {
		if clusterMinHealthyPercentage, ok := clusterMinHealthyPercentage.(map[string]interface{}); ok {
			healthcheck.ClusterMinHealthyPercentage = make(map[string]float32)
			for cluster, percent := range clusterMinHealthyPercentage {
				healthcheck.ClusterMinHealthyPercentage[cluster] = float32(percent.(float64))
			}
		} else {
			log.StartLogger.Fatalln("[passthrough] in health check filter config is not bool")
		}
	} else {
		log.StartLogger.Fatalln("[passthrough] is required in healthcheck filter config")
	}
	return healthcheck
}

func ParseListenerConfig(c *ListenerConfig, inheritListeners []*v2.ListenerConfig) *v2.ListenerConfig {
	if c.Name == "" {
		log.StartLogger.Fatalln("[name] is required in listener config")
	}

	if c.Address == "" {
		log.StartLogger.Fatalln("[Address] is required in listener config")
	}
	addr, err := net.ResolveTCPAddr("tcp", c.Address)

	if err != nil {
		log.StartLogger.Fatalln("[Address] not valid:" + c.Address)
	}

	//try inherit legacy listener
	var old *net.TCPListener

	for _, il := range inheritListeners {
		if il.Addr.String() == addr.String() {
			log.StartLogger.Infof("inherit listener addr: %s", c.Address)
			old = il.InheritListener
			il.Remain = true
			break
		}
	}

	return &v2.ListenerConfig{
		Name:                                  c.Name,
		Addr:                                  addr,
		BindToPort:                            c.BindToPort,
		InheritListener:                       old,
		PerConnBufferLimitBytes:               1 << 15,
		LogPath:                               c.LogPath,
		LogLevel:                              uint8(ParseLogLevel(c.LogLevel)),
		AccessLogs:                            ParseAccessConfig(c.AccessLogs),
		DisableConnIo:                         c.DisableConnIo,
		HandOffRestoredDestinationConnections: c.HandOffRestoredDestinationConnections,
		FilterChains:                          ParseFilterChains(c.FilterChains),
	}
}

func ParseClusterConfig(clusters []ClusterConfig) ([]v2.Cluster, map[string][]v2.Host) {
	var clustersV2 []v2.Cluster
	clusterV2Map := make(map[string][]v2.Host)

	for _, c := range clusters {
		// cluster name
		if c.Name == "" {
			log.StartLogger.Fatalln("[name] is required in cluster config")
		}

		var clusterType v2.ClusterType

		//cluster type
		if c.Type == "" {
			log.StartLogger.Fatalln("[type] is required in cluster config")
		} else {
			if ct, ok := clusterTypeMap[c.Type]; ok {
				clusterType = ct
			} else {
				log.StartLogger.Fatalln("unknown cluster type:", c.Type)
			}
		}

		var lbType v2.LbType

		if c.LbType == "" {
			log.StartLogger.Fatalln("[lb_type] is required in cluster config")
		} else {
			if lt, ok := lbTypeMap[c.LbType]; ok {
				lbType = lt
			} else {
				log.StartLogger.Fatalln("unknown lb type:", c.LbType)
			}
		}

		if c.MaxRequestPerConn == 0 {
			c.MaxRequestPerConn = 1024
			log.StartLogger.Infof("[max_request_per_conn] is not specified, use default value %d", 1024)
		}

		if c.ConnBufferLimitBytes == 0 {
			c.ConnBufferLimitBytes = 16 * 1026
			log.StartLogger.Infof("[conn_buffer_limit_bytes] is not specified, use default value %d", 1024*16)
		}

		//clusterSpec := c.ClusterSpecConfig.(ClusterSpecConfig)
		clusterSpec := c.ClusterSpecConfig

		// checkout LBSubsetConfig
		if c.LBSubsetConfig.FallBackPolicy > 2 {
			log.StartLogger.Panic("lb subset config 's fall back policy set error. " +
				"For 0, represent NO_FALLBACK" +
				"For 1, reprenst ANY_ENDPOINT" +
				"For 2, reprenst DEFAULT_SUBSET")
		}

		//v2.Cluster
		clusterV2 := v2.Cluster{
			Name:                 c.Name,
			ClusterType:          clusterType,
			LbType:               lbType,
			MaxRequestPerConn:    c.MaxRequestPerConn,
			ConnBufferLimitBytes: c.ConnBufferLimitBytes,

			HealthCheck:      ParseClusterHealthCheckConf(&c.HealthCheck),
			CirBreThresholds: ParseCircuitBreakers(c.CircuitBreakers),

			Spec:           ParseConfigSpecConfig(&clusterSpec),
			LBSubSetConfig: c.LBSubsetConfig,
			TLS:            ParseTLSConfig(&c.TLS),
		}

		clustersV2 = append(clustersV2, clusterV2)
		hostV2 := ParseHostConfig(&c)
		clusterV2Map[c.Name] = hostV2
	}

	// trigger all callbacks
	if cbs, ok := configParsedCBMaps[ParseCallbackKeyCluster]; ok {
		for _, cb := range cbs {
			cb(clustersV2, false)
		}
	}
	return clustersV2, clusterV2Map
}

func ParseClusterHealthCheckConf(c *ClusterHealthCheckConfig) v2.HealthCheck {

	var healthcheckInstance v2.HealthCheck

	if c.Protocol == "" {
		log.StartLogger.Warnf("healthcheck for cluster is disabled")

	} else if _, ok := ProtocolsSupported[c.Protocol]; ok {
		healthcheckInstance = v2.HealthCheck{
			Protocol:           c.Protocol,
			Timeout:            c.Timeout.Duration,
			Interval:           c.Interval.Duration,
			IntervalJitter:     c.IntervalJitter.Duration,
			HealthyThreshold:   c.HealthyThreshold,
			UnhealthyThreshold: c.UnhealthyThreshold,
			CheckPath:          c.CheckPath,
			ServiceName:        c.ServiceName,
		}
	} else {
		log.StartLogger.Fatal("unsuppoted health check protocol:", c.Protocol)
	}

	return healthcheckInstance
}

func ParseCircuitBreakers(cbcs []*CircuitBreakerdConfig) v2.CircuitBreakers {
	var cb v2.CircuitBreakers
	var rp v2.RoutingPriority

	for _, cbc := range cbcs {
		if strings.ToLower(cbc.Priority) == "default" {
			rp = v2.DEFAULT
		} else {
			rp = v2.HIGH
		}

		if 0 == cbc.MaxConnections || 0 == cbc.MaxPendingRequests ||
			0 == cbc.MaxRequests || 0 == cbc.MaxRetries {
			log.StartLogger.Warnf("zero is set in circuitBreakers' config")
		}

		threshold := v2.Thresholds{
			Priority:           rp,
			MaxConnections:     cbc.MaxConnections,
			MaxPendingRequests: cbc.MaxPendingRequests,
			MaxRequests:        cbc.MaxRequests,
			MaxRetries:         cbc.MaxRetries,
		}

		cb.Thresholds = append(cb.Thresholds, threshold)
	}

	return cb
}

func ParseConfigSpecConfig(c *ClusterSpecConfig) v2.ClusterSpecInfo {
	var specs []v2.SubscribeSpec

	for _, sub := range c.Subscribes {
		specs = append(specs, v2.SubscribeSpec{
			ServiceName: sub.ServiceName,
		})
	}

	return v2.ClusterSpecInfo{
		Subscribes: specs,
	}
}

func ParseHostConfig(c *ClusterConfig) []v2.Host {
	// host maybe nil when rewriting config
	//if c.Hosts == nil || len(c.Hosts) == 0 {
	//	log.StartLogger.Debugf("[hosts] is required in cluster config")
	//}
	var hosts []v2.Host

	for _, host := range c.Hosts {

		if host.Address == "" {
			log.StartLogger.Fatalln("[host.address] is required in host config")
		}

		hosts = append(hosts, host)
	}

	return hosts
}

func ParseServiceRegistry(src ServiceRegistryConfig) {
	var SrvRegInfo v2.ServiceRegistryInfo

	if src.ServiceAppInfo.AppName == "" {
		//log.StartLogger.Debugf("[ParseServiceRegistry] appname is nil")
	}

	srvappinfo := v2.ApplicationInfo{
		AntShareCloud: src.ServiceAppInfo.AntShareCloud,
		DataCenter:    src.ServiceAppInfo.DataCenter,
		AppName:       src.ServiceAppInfo.AppName,
	}

	var SrvPubInfoArray []v2.PublishInfo

	for _, pubs := range src.ServicePubInfo {
		SrvPubInfoArray = append(SrvPubInfoArray, v2.PublishInfo{
			Pub: v2.PublishContent{
				ServiceName: pubs.ServiceName,
				PubData:     pubs.PubData,
			},
		})
	}

	SrvRegInfo = v2.ServiceRegistryInfo{
		srvappinfo,
		SrvPubInfoArray,
	}

	//trigger all callbacks
	if cbs, ok := configParsedCBMaps[ParseCallbackKeyServiceRgtInfo]; ok {
		for _, cb := range cbs {
			cb(SrvRegInfo, true)
		}
	}
}
