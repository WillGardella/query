package clustering_zk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/couchbaselabs/query/accounting"
	"github.com/couchbaselabs/query/clustering"
	"github.com/couchbaselabs/query/datastore"
	"github.com/couchbaselabs/query/errors"
	"github.com/samuel/go-zookeeper/zk"
)

const _PREFIX = "zookeeper:"
const _RESERVED_NAME = "zookeeper"

// zkConfigStore implements clustering.ConfigurationStore
type zkConfigStore struct {
	conn *zk.Conn
	url  string
}

// create a zkConfigStore given the path to a zookeeper instance
func NewConfigstore(path string) (clustering.ConfigurationStore, errors.Error) {
	if strings.HasPrefix(path, _PREFIX) {
		path = path[len(_PREFIX):]
	}
	zks := strings.Split(path, ",")
	conn, _, err := zk.Connect(zks, time.Second)
	if err != nil {
		return nil, errors.NewError(err, "")
	}
	return &zkConfigStore{
		conn: conn,
		url:  path,
	}, nil
}

// Implement Stringer interface
func (z *zkConfigStore) String() string {
	return fmt.Sprintf("url=%v", z.url)
}

// Implement clustering.ConfigurationStore interface
func (z *zkConfigStore) Id() string {
	return z.URL()
}

func (z *zkConfigStore) URL() string {
	return "zookeeper:" + z.url
}

func (z *zkConfigStore) ClusterNames() ([]string, errors.Error) {
	clusterIds := []string{}
	nodes, _, err := z.conn.Children("/")
	if err != nil {
		return nil, errors.NewError(err, "")
	}
	for _, name := range nodes {
		clusterIds = append(clusterIds, name)
	}
	return clusterIds, nil
}

func (z *zkConfigStore) ClusterByName(name string) (clustering.Cluster, errors.Error) {
	data, _, err := z.conn.Get("/" + name)
	if err != nil {
		return nil, errors.NewError(err, "")
	}
	var clusterConfig zkCluster
	err = json.Unmarshal(data, &clusterConfig)
	if err != nil {
		return nil, errors.NewError(err, "")
	}
	clusterConfig.configStore = z
	return &clusterConfig, nil
}

func (z *zkConfigStore) ConfigurationManager() clustering.ConfigurationManager {
	return z
}

// zkConfigStore also implements clustering.ConfigurationManager interface
func (z *zkConfigStore) ConfigurationStore() clustering.ConfigurationStore {
	return z
}

func (z *zkConfigStore) AddCluster(c clustering.Cluster) (clustering.Cluster, errors.Error) {
	flags := int32(0)
	acl := zk.WorldACL(zk.PermAll) // TODO: expose authentication in the API
	clusterBytes, err := json.Marshal(c)
	if err != nil {
		return nil, errors.NewError(err, "")
	}
	_, err = z.conn.Create("/"+c.Name(), clusterBytes, flags, acl)
	if err != nil {
		return nil, errors.NewError(err, "")
	}
	return c, nil
}

func (z *zkConfigStore) RemoveCluster(c clustering.Cluster) (bool, errors.Error) {
	return z.RemoveClusterByName(c.Name())
}

func (z *zkConfigStore) RemoveClusterByName(name string) (bool, errors.Error) {
	err := z.conn.Delete("/"+name, 0)
	if err != nil {
		return false, errors.NewError(err, "")
	} else {
		return true, nil
	}

}

func (z *zkConfigStore) GetClusters() ([]clustering.Cluster, errors.Error) {
	clusters := []clustering.Cluster{}
	nodes, _, err := z.conn.Children("/")
	if err != nil {
		return nil, errors.NewError(err, "")
	}
	for _, name := range nodes {
		if name == _RESERVED_NAME {
			continue
		}
		data, _, err := z.conn.Get("/" + name)
		if err != nil {
			return nil, errors.NewError(err, "")
		}
		cluster := &zkCluster{}
		err = json.Unmarshal(data, cluster)
		if err != nil {
			return nil, errors.NewError(err, "")
		}
		clusters = append(clusters, cluster)
	}
	return clusters, nil
}

// zkCluster implements clustering.Cluster
type zkCluster struct {
	configStore    clustering.ConfigurationStore `json:"-"`
	dataStore      datastore.Datastore           `json:"-"`
	acctStore      accounting.AccountingStore    `json:"-"`
	ClusterName    string                        `json:"name"`
	DatastoreURI   string                        `json:"datastore_uri"`
	ConfigstoreURI string                        `json:"configstore_uri"`
	AccountingURI  string                        `json:"accountstore_uri"`
	version        clustering.Version            `json:"-"`
	VersionString  string                        `json:"version"`
}

// Create a new zkCluster instance
func NewCluster(name string,
	version clustering.Version,
	configstore clustering.ConfigurationStore,
	datastore datastore.Datastore,
	acctstore accounting.AccountingStore) (clustering.Cluster, errors.Error) {
	c := makeZkCluster(name, version, configstore, datastore, acctstore)
	return c, nil
}

func makeZkCluster(name string,
	version clustering.Version,
	cs clustering.ConfigurationStore,
	ds datastore.Datastore,
	as accounting.AccountingStore) clustering.Cluster {
	cluster := zkCluster{
		configStore:    cs,
		dataStore:      ds,
		acctStore:      as,
		ClusterName:    name,
		DatastoreURI:   ds.URL(),
		ConfigstoreURI: cs.URL(),
		AccountingURI:  as.URL(),
		version:        version,
		VersionString:  version.String(),
	}
	return &cluster
}

// zkCluster implements Stringer interface
func (z *zkCluster) String() string {
	return getJsonString(z)
}

// zkCluster implements clustering.Cluster interface
func (z *zkCluster) ConfigurationStoreId() string {
	return z.configStore.Id()
}

func (z *zkCluster) Name() string {
	return z.ClusterName
}

func (z *zkCluster) QueryNodeNames() ([]string, errors.Error) {
	queryNodeNames := []string{}
	impl, ok := getConfigStoreImplementation(z)
	if !ok {
		return nil, errors.NewWarning(fmt.Sprintf("Unable to connect to zookeeper at %s", z.ConfigurationStoreId()))
	}
	nodes, _, err := impl.conn.Children("/" + z.ClusterName)
	if err != nil {
		return nil, errors.NewError(err, "")
	}
	for _, name := range nodes {
		queryNodeNames = append(queryNodeNames, name)
	}
	return queryNodeNames, nil
}

func (z *zkCluster) QueryNodeByName(name string) (clustering.QueryNode, errors.Error) {
	impl, ok := getConfigStoreImplementation(z)
	if !ok {
		return nil, errors.NewWarning(fmt.Sprintf("Unable to connect to zookeeper at %s", z.ConfigurationStoreId()))
	}
	data, _, err := impl.conn.Get("/" + z.ClusterName + "/" + name)
	if err != nil {
		return nil, errors.NewError(err, "")
	}
	var queryNode zkQueryNodeConfig
	err = json.Unmarshal(data, &queryNode)
	if err != nil {
		return nil, errors.NewError(err, "")
	}
	return &queryNode, nil
}

func (z *zkCluster) Datastore() datastore.Datastore {
	return z.dataStore
}

func (z *zkCluster) AccountingStore() accounting.AccountingStore {
	return z.acctStore
}

func (z *zkCluster) ConfigurationStore() clustering.ConfigurationStore {
	return z.configStore
}

func (z *zkCluster) Version() clustering.Version {
	if z.version == nil {
		z.version = clustering.NewVersion(z.VersionString)
	}
	return z.version
}

// internal function to get a handle on the zookeeper connection
func getConfigStoreImplementation(z *zkCluster) (impl *zkConfigStore, ok bool) {
	impl, ok = z.configStore.(*zkConfigStore)
	return
}

func (z *zkCluster) ClusterManager() clustering.ClusterManager {
	return z
}

// zkCluster implements clustering.ClusterManager interface
func (z *zkCluster) Cluster() clustering.Cluster {
	return z
}

func (z *zkCluster) AddQueryNode(n clustering.QueryNode) (clustering.QueryNode, errors.Error) {
	impl, ok := getConfigStoreImplementation(z)
	if !ok {
		return nil, errors.NewWarning(fmt.Sprintf("Unable to connect to zookeeper at %s", z.ConfigurationStoreId()))
	}
	// Check that query node has compatible backend connections:
	if n.Standalone().Datastore().URL() != z.DatastoreURI {
		return nil, errors.NewWarning(fmt.Sprintf("Failed to add Query Node %v: incompatible datastore with cluster %s", n, z.DatastoreURI))
	}
	if n.Standalone().ConfigurationStore().URL() != z.ConfigstoreURI {
		return nil, errors.NewWarning(fmt.Sprintf("Failed to add Query Node %v: incompatible configstore with cluster %s", n, z.ConfigstoreURI))
	}
	// Check that query node is version compatible with the cluster:
	if !z.Version().Compatible(n.Standalone().Version()) {
		return nil, errors.NewWarning(fmt.Sprintf("Failed to add Query Node %v: not version compatible with cluster (%v)", n, z.Version()))
	}
	qryNodeImpl, ok := n.(*zkQueryNodeConfig)
	if !ok {
		return nil, errors.NewWarning(fmt.Sprintf("Failed to add Query Node %v: cannot set cluster reference", n))
	}
	// The query node can be accepted into the cluster - set its cluster reference and name and unset its Standalone:
	qryNodeImpl.ClusterRef = z
	qryNodeImpl.ClusterName = z.Name()
	qryNodeImpl.StandaloneRef = nil
	// Add entry for query node: ephemeral node
	flags := int32(zk.FlagEphemeral)
	acl := zk.WorldACL(zk.PermAll) // TODO: credentials - expose in the API
	key := "/" + z.Name() + "/" + n.Name()
	value, err := json.Marshal(n)
	if err != nil {
		return nil, errors.NewError(err, "")
	}
	_, err = impl.conn.Create(key, value, flags, acl)
	if err != nil {
		return nil, errors.NewError(err, "")
	}
	return n, nil
}

func (z *zkCluster) RemoveQueryNode(n clustering.QueryNode) (clustering.QueryNode, errors.Error) {
	return z.RemoveQueryNodeByName(n.Name())
}

func (z *zkCluster) RemoveQueryNodeByName(name string) (clustering.QueryNode, errors.Error) {
	impl, ok := getConfigStoreImplementation(z)
	if !ok {
		return nil, errors.NewWarning(fmt.Sprintf("Unable to connect to zookeeper at %s", z.ConfigurationStoreId()))
	}
	err := impl.conn.Delete("/"+z.Name()+"/"+name, 0)
	if err != nil {
		return nil, errors.NewError(err, "")
	}
	return nil, nil
}

func (z *zkCluster) GetQueryNodes() ([]clustering.QueryNode, errors.Error) {
	impl, ok := getConfigStoreImplementation(z)
	if !ok {
		return nil, errors.NewWarning(fmt.Sprintf("Unable to connect to zookeeper at %s", z.ConfigurationStoreId()))
	}
	qryNodes := []clustering.QueryNode{}
	nodes, _, err := impl.conn.Children("/" + z.Name())
	if err != nil {
		return nil, errors.NewError(err, "")
	}
	for _, name := range nodes {
		data, _, err := impl.conn.Get("/" + z.Name() + "/" + name)
		if err != nil {
			return nil, errors.NewError(err, "")
		}
		queryNode := &zkQueryNodeConfig{}
		err = json.Unmarshal(data, queryNode)
		if err != nil {
			return nil, errors.NewError(err, "")
		}
		qryNodes = append(qryNodes, queryNode)
	}
	return qryNodes, nil
}

// zkQueryNodeConfig implements clustering.QueryNode
type zkQueryNodeConfig struct {
	ClusterName      string                    `json:"cluster_name"`
	QueryNodeName    string                    `json:"name"`
	QueryEndpointURL string                    `json:"query_endpoint"`
	AdminEndpointURL string                    `json:"admin_endpoint"`
	ClusterRef       *zkCluster                `json:"-"`
	StandaloneRef    *clustering.StdStandalone `json:"-"`
	OptionsCL        *clustering.ClOptions     `json:"options"`
}

// Create a query node configuration
func NewQueryNode(query_addr string,
	stndln *clustering.StdStandalone,
	opts *clustering.ClOptions) (clustering.QueryNode, errors.Error) {
	ip_addr, err := externalIP()
	if err != nil {
		ip_addr = "127.0.0.1"
	}
	// Construct query node name from ip addr and http_addr. Assumption that this will be unique
	queryName := ip_addr + query_addr
	queryEndpoint := "http://" + queryName + "/query"
	// TODO : protocol specification: how do we know it will be http?
	adminEndpoint := "http://" + queryName + "/admin"
	return makeZkQueryNodeConfig("", queryName, queryEndpoint, adminEndpoint, stndln, opts), nil
}

func makeZkQueryNodeConfig(ClusterName string,
	Name string,
	queryEndpoint string,
	adminEndpoint string,
	standalone *clustering.StdStandalone,
	opts *clustering.ClOptions) clustering.QueryNode {
	node := zkQueryNodeConfig{
		ClusterName:      ClusterName,
		QueryNodeName:    Name,
		QueryEndpointURL: queryEndpoint,
		AdminEndpointURL: adminEndpoint,
		ClusterRef:       nil,
		StandaloneRef:    standalone,
		OptionsCL:        opts,
	}
	return &node
}

// zkQueryNodeConfig implements Stringer interface
func (z *zkQueryNodeConfig) String() string {
	return getJsonString(z)
}

// zkQueryNodeConfig implements clustering.QueryNode interface
func (z *zkQueryNodeConfig) Cluster() clustering.Cluster {
	return z.ClusterRef
}

func (z *zkQueryNodeConfig) Name() string {
	return z.QueryNodeName
}

func (z *zkQueryNodeConfig) QueryEndpoint() string {
	return z.QueryEndpointURL
}

func (z *zkQueryNodeConfig) ClusterEndpoint() string {
	return z.AdminEndpointURL
}

func (z *zkQueryNodeConfig) Standalone() clustering.Standalone {
	return z.StandaloneRef
}

func (z *zkQueryNodeConfig) Options() clustering.QueryNodeOptions {
	return z.OptionsCL
}

// helper function to determine the external IP address of a query node -
// used to create a name for the query node in NewQueryNode function.
func externalIP() (string, errors.Error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", errors.NewError(err, "")
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue // interface down
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue // loopback interface
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return "", errors.NewError(err, "")
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue // not an ipv4 address
			}
			return ip.String(), nil
		}
	}
	return "", errors.NewError(nil, "Not connected to the network")
}

func getJsonString(i interface{}) string {
	serialized, _ := json.Marshal(i)
	s := bytes.NewBuffer(append(serialized, '\n'))
	return s.String()
}