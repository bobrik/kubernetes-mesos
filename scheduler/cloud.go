package scheduler

import "errors"
import "net"
import log "github.com/golang/glog"
import "github.com/GoogleCloudPlatform/kubernetes/pkg/api"
import cloud "github.com/GoogleCloudPlatform/kubernetes/pkg/cloudprovider"

var (
	noHostNameSpecified = errors.New("No hostname specified")
)

type MesosCloud struct {
	*KubernetesScheduler
}

// implementation of cloud.Interface; Mesos natively provides minimal cloud-type resources.
// More robust cloud support requires a combination of Mesos and cloud-specific knowledge,
// which will likely never be present in this vanilla implementation.
func (c *MesosCloud) Instances() (cloud.Instances, bool) {
	return c, true
}

// implementation of cloud.Interface; Mesos does not provide any type of native load
// balancing by default, so this implementation always returns (nil,false).
func (c *MesosCloud) TCPLoadBalancer() (cloud.TCPLoadBalancer, bool) {
	return nil, false
}

// implementation of cloud.Interface; Mesos does not provide any type of native region
// or zone awareness, so this implementation always returns (nil,false).
func (c *MesosCloud) Zones() (cloud.Zones, bool) {
	return nil, false
}

// implementation of cloud.Instances.
// IPAddress returns an IP address of the specified instance.
func (c *MesosCloud) IPAddress(name string) (net.IP, error) {
	if name == "" {
		return nil, noHostNameSpecified
	}
	// TODO(jdef): validate that name actually points to a slave that we know
	if iplist, err := net.LookupIP(name); err != nil {
		log.Warningf("Failed to resolve IP from host name '%v': %v", name, err)
		return nil, err
	} else {
		ipaddr := iplist[0]
		log.V(2).Infof("Resolved host '%v' to '%v'", name, ipaddr)
		return ipaddr, nil
	}
}

// implementation of cloud.Instances; does not implement any filtering.
// List lists instances that match 'filter' which is a regular expression which must match the entire instance name (fqdn).
func (c *MesosCloud) List(filter string) ([]string, error) {
	c.RLock()
	defer c.RUnlock()

	var slaveHosts []string
	for _, slave := range c.slaves {
		slaveHosts = append(slaveHosts, slave.HostName)
	}
	return slaveHosts, nil
}

// implementation of cloud.Instances; always returns nil,nil.
// GetNodeResources gets the resources for a particular node
func (c *MesosCloud) GetNodeResources(name string) (*api.NodeResources, error) {
	return nil, nil
}
