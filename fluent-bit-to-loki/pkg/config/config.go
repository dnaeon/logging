package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/logging"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/promtail/client"
	lokiflag "github.com/grafana/loki/pkg/util/flagext"
)

var defaultClientCfg = client.Config{}

func init() {
	// Init everything with default values.
	flagext.RegisterFlags(&defaultClientCfg)
}

// Getter get a configuration settings base on the passed key
type Getter interface {
	Get(key string) string
}

// Format is the log line format
type Format int

const (
	// JSONFormat represents json format for log line
	JSONFormat Format = iota
	// KvPairFormat represents key-value format for log line
	KvPairFormat
)

const (
	falseStr = "false"
	trueStr  = "true"
)

//Config holds all of the needet properties of the loki output plugin
type Config struct {
	ClientConfig         client.Config
	BufferConfig         BufferConfig
	LogLevel             logging.Level
	AutoKubernetesLabels bool
	RemoveKeys           []string
	LabelKeys            []string
	LineFormat           Format
	DropSingleKey        bool
	LabelMap             map[string]interface{}
	DynamicHostPath      map[string]interface{}
	DynamicHostPrefix    string
	DynamicHostSulfix    string
	DynamicHostRegex     string
}

// BufferConfig contains the buffer settings
type BufferConfig struct {
	Buffer     bool
	BufferType string
	DqueConfig DqueConfig
}

// DqueConfig contains the dqueue settings
type DqueConfig struct {
	QueueDir         string
	QueueSegmentSize int
	QueueSync        bool
	QueueName        string
}

var DefaultBufferConfig = BufferConfig{
	Buffer:     false,
	BufferType: "dque",
	DqueConfig: DefaultDqueConfig,
}

var DefaultDqueConfig = DqueConfig{
	QueueDir:         "/tmp/flb-storage/loki",
	QueueSegmentSize: 500,
	QueueSync:        false,
	QueueName:        "dque",
}

// ParseConfig parse a Loki plugin configuration
func ParseConfig(cfg Getter) (*Config, error) {
	res := &Config{}

	res.ClientConfig = defaultClientCfg
	res.BufferConfig = DefaultBufferConfig

	url := cfg.Get("URL")
	var clientURL flagext.URLValue
	if url == "" {
		url = "http://localhost:3100/loki/api/v1/push"
	}
	err := clientURL.Set(url)
	if err != nil {
		return nil, errors.New("failed to parse client URL")
	}
	res.ClientConfig.URL = clientURL

	// cfg.Get will return empty string if not set, which is handled by the client library as no tenant
	res.ClientConfig.TenantID = cfg.Get("TenantID")

	batchWait := cfg.Get("BatchWait")
	if batchWait != "" {
		batchWaitValue, err := strconv.Atoi(batchWait)
		if err != nil {
			return nil, fmt.Errorf("failed to parse BatchWait: %s", batchWait)
		}
		res.ClientConfig.BatchWait = time.Duration(batchWaitValue) * time.Second
	}

	batchSize := cfg.Get("BatchSize")
	if batchSize != "" {
		batchSizeValue, err := strconv.Atoi(batchSize)
		if err != nil {
			return nil, fmt.Errorf("failed to parse BatchSize: %s", batchSize)
		}
		res.ClientConfig.BatchSize = batchSizeValue
	}

	labels := cfg.Get("Labels")
	if labels == "" {
		labels = `{job="fluent-bit"}`
	}
	matchers, err := logql.ParseMatchers(labels)
	if err != nil {
		return nil, err
	}
	labelSet := make(model.LabelSet)
	for _, m := range matchers {
		labelSet[model.LabelName(m.Name)] = model.LabelValue(m.Value)
	}
	res.ClientConfig.ExternalLabels = lokiflag.LabelSet{LabelSet: labelSet}

	logLevel := cfg.Get("LogLevel")
	if logLevel == "" {
		logLevel = "info"
	}
	var level logging.Level
	if err := level.Set(logLevel); err != nil {
		return nil, fmt.Errorf("invalid log level: %v", logLevel)
	}
	res.LogLevel = level

	autoKubernetesLabels := cfg.Get("AutoKubernetesLabels")
	switch autoKubernetesLabels {
	case "false", "":
		res.AutoKubernetesLabels = false
	case "true":
		res.AutoKubernetesLabels = true
	default:
		return nil, fmt.Errorf("invalid boolean AutoKubernetesLabels: %v", autoKubernetesLabels)
	}

	removeKey := cfg.Get("RemoveKeys")
	if removeKey != "" {
		res.RemoveKeys = strings.Split(removeKey, ",")
	}

	labelKeys := cfg.Get("LabelKeys")
	if labelKeys != "" {
		res.LabelKeys = strings.Split(labelKeys, ",")
	}

	dropSingleKey := cfg.Get("DropSingleKey")
	switch dropSingleKey {
	case "false":
		res.DropSingleKey = false
	case "true", "":
		res.DropSingleKey = true
	default:
		return nil, fmt.Errorf("invalid boolean DropSingleKey: %v", dropSingleKey)
	}

	lineFormat := cfg.Get("LineFormat")
	switch lineFormat {
	case "json", "":
		res.LineFormat = JSONFormat
	case "key_value":
		res.LineFormat = KvPairFormat
	default:
		return nil, fmt.Errorf("invalid format: %s", lineFormat)
	}

	labelMapPath := cfg.Get("LabelMapPath")
	if labelMapPath != "" {
		content, err := ioutil.ReadFile(labelMapPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open LabelMap file: %s", err)
		}
		if err := json.Unmarshal(content, &res.LabelMap); err != nil {
			return nil, fmt.Errorf("failed to Unmarshal LabelMap file: %s", err)
		}
		res.LabelKeys = nil
	}

	dynamicHostPath := cfg.Get("DynamicHostPath")
	if dynamicHostPath != "" {
		if err := json.Unmarshal([]byte(dynamicHostPath), &res.DynamicHostPath); err != nil {
			return nil, fmt.Errorf("failed to Unmarshal DynamicHostPath json: %s", err)
		}
	}

	res.DynamicHostPrefix = cfg.Get("DynamicHostPrefix")
	res.DynamicHostSulfix = cfg.Get("DynamicHostSulfix")
	res.DynamicHostRegex = cfg.Get("DynamicHostRegex")
	if res.DynamicHostRegex == "" {
		res.DynamicHostRegex = "*"
	}

	maxRetries := cfg.Get("MaxRetries")
	if maxRetries != "" {
		res.ClientConfig.BackoffConfig.MaxRetries, err = strconv.Atoi(maxRetries)
		if err != nil {
			return nil, fmt.Errorf("failed to parse MaxRetries: %s", maxRetries)
		}
	}

	timeout := cfg.Get("Timeout")
	if timeout != "" {
		t, err := strconv.Atoi(timeout)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Timeout: %s", timeout)
		}
		res.ClientConfig.Timeout = time.Duration(t) * time.Second
	}

	minBackoff := cfg.Get("MinBackoff")
	if minBackoff != "" {
		mib, err := strconv.Atoi(minBackoff)
		if err != nil {
			return nil, fmt.Errorf("failed to parse MinBackoff: %s", minBackoff)
		}
		res.ClientConfig.BackoffConfig.MinBackoff = time.Duration(mib) * time.Second
	}

	maxBackoff := cfg.Get("MaxBackoff")
	if maxBackoff != "" {
		mab, err := strconv.Atoi(minBackoff)
		if err != nil {
			return nil, fmt.Errorf("failed to parse MaxBackoff: %s", maxBackoff)
		}
		res.ClientConfig.BackoffConfig.MaxBackoff = time.Duration(mab) * time.Second
	}

	// enable loki plugin buffering
	buffer := cfg.Get("Buffer")
	switch buffer {
	case falseStr, "":
		res.BufferConfig.Buffer = false
	case trueStr:
		res.BufferConfig.Buffer = true
	default:
		return nil, fmt.Errorf("invalid boolean Buffer: %v", buffer)
	}

	// buffering type
	bufferType := cfg.Get("BufferType")
	if bufferType != "" {
		res.BufferConfig.BufferType = bufferType
	}

	// dque directory
	queueDir := cfg.Get("DqueDir")
	if queueDir != "" {
		res.BufferConfig.DqueConfig.QueueDir = queueDir
	}

	// dque segment size (queueEntry unit)
	queueSegmentSize := cfg.Get("DqueSegmentSize")
	if queueSegmentSize != "" {
		res.BufferConfig.DqueConfig.QueueSegmentSize, err = strconv.Atoi(queueSegmentSize)
		if err != nil {
			return nil, fmt.Errorf("impossible to convert string to integer DqueSegmentSize: %v", queueSegmentSize)
		}
	}

	// dque control file change sync to disk as they happen aka dque.turbo mode
	queueSync := cfg.Get("DqueSync")
	switch queueSync {
	case "normal", "":
		res.BufferConfig.DqueConfig.QueueSync = false
	case "full":
		res.BufferConfig.DqueConfig.QueueSync = true
	default:
		return nil, fmt.Errorf("invalid string queueSync: %v", queueSync)
	}

	// dque name
	queueName := cfg.Get("DqueName")
	if queueName != "" {
		res.BufferConfig.DqueConfig.QueueName = queueName
	}

	return res, nil
}
