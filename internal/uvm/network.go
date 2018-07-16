package uvm

import (
	"fmt"
	"path"

	"github.com/Microsoft/hcsshim/internal/guestrequest"
	"github.com/Microsoft/hcsshim/internal/guid"
	"github.com/Microsoft/hcsshim/internal/hns"
	"github.com/Microsoft/hcsshim/internal/requesttype"
	"github.com/Microsoft/hcsshim/internal/resourcetype"
	"github.com/Microsoft/hcsshim/internal/schema2"
	"github.com/sirupsen/logrus"
)

func (uvm *UtilityVM) AddNetNS(id string, endpoints []*hns.HNSEndpoint) (err error) {
	uvm.m.Lock()
	defer uvm.m.Unlock()
	ns := uvm.namespaces[id]
	if ns == nil {
		ns = &namespaceInfo{}
		defer func() {
			if err != nil {
				if e := uvm.removeNamespaceNICs(ns); e != nil {
					logrus.Warn("failed to undo NIC add: %s", e)
				}
			}
		}()
		for _, endpoint := range endpoints {
			nicID := guid.New()
			err = uvm.addNIC(nicID, endpoint)
			if err != nil {
				return err
			}
			ns.nics = append(ns.nics, nicInfo{nicID, endpoint})
		}
		if uvm.namespaces == nil {
			uvm.namespaces = make(map[string]*namespaceInfo)
		}
		uvm.namespaces[id] = ns
	}
	ns.refCount++
	return nil
}

func (uvm *UtilityVM) RemoveNetNS(id string) error {
	uvm.m.Lock()
	defer uvm.m.Unlock()
	ns := uvm.namespaces[id]
	if ns == nil || ns.refCount <= 0 {
		panic(fmt.Errorf("removed a namespace that was not added: %s", id))
	}
	ns.refCount--
	var err error
	if ns.refCount == 0 {
		err = uvm.removeNamespaceNICs(ns)
		delete(uvm.namespaces, id)
	}
	return err
}

func (uvm *UtilityVM) removeNamespaceNICs(ns *namespaceInfo) error {
	for len(ns.nics) != 0 {
		nic := ns.nics[len(ns.nics)-1]
		err := uvm.removeNIC(nic.ID, nic.Endpoint)
		if err != nil {
			return err
		}
		ns.nics = ns.nics[:len(ns.nics)-1]
	}
	return nil
}

func (uvm *UtilityVM) addNIC(id guid.GUID, endpoint *hns.HNSEndpoint) error {

	// First a pre-add. This is a guest-only request.
	preAddRequest := hcsschema.ModifySettingRequest{
		GuestRequest: guestrequest.GuestRequest{
			ResourceType: guestrequest.ResourceTypeNetwork,
			RequestType:  requesttype.Add,
			Settings: guestrequest.NetworkModifyRequest{ // TODO Swagger Refresh. HCS.Schema.Requests.NetworkModifySettingRequest
				AdapterInstanceId: id.String(),
				RequestType:       requesttype.PreAdd,
				Settings:          endpoint,
			},
		},
	}
	if err := uvm.Modify(&preAddRequest); err != nil {
		return err
	}

	// Then the Add itself
	request := hcsschema.ModifySettingRequest{
		ResourcePath: path.Join("VirtualMachine/Devices/NetworkAdapters", id.String()),
		ResourceType: resourcetype.Network,
		RequestType:  requesttype.Add,
		Settings: hcsschema.NetworkAdapter{
			EndpointId: endpoint.Id,
			MacAddress: endpoint.MacAddress,
		},
		GuestRequest: guestrequest.GuestRequest{
			ResourceType: guestrequest.ResourceTypeNetwork,
			RequestType:  requesttype.Add,
			Settings: guestrequest.NetworkModifyRequest{ // TODO Swagger Refresh. HCS.Schema.Requests.NetworkModifySettingRequest
				AdapterInstanceId: id.String(),
				RequestType:       requesttype.Add,
			},
		},
	}
	if err := uvm.Modify(&request); err != nil {
		return err
	}
	return nil
}

func (uvm *UtilityVM) removeNIC(id guid.GUID, endpoint *hns.HNSEndpoint) error {
	request := hcsschema.ModifySettingRequest{
		ResourcePath: path.Join("VirtualMachine/Devices/NetworkAdapters", id.String()),
		ResourceType: resourcetype.Network,
		RequestType:  requesttype.Remove,
		Settings: hcsschema.NetworkAdapter{
			EndpointId: endpoint.Id,
			MacAddress: endpoint.MacAddress,
		},
		GuestRequest: hcsschema.ModifySettingRequest{ // @beweedon Is this right? In podscenariotests.ps1...? Not NetworkModifyRequest?
			ResourceType: resourcetype.Network, // guestrequest.ResourceTypeNetwork if NetworkModifyRequest
			RequestType:  requesttype.Remove,
			Settings: guestrequest.NetworkModifyRequest{ // TODO Swagger Refresh. HCS.Schema.Requests.NetworkModifySettingRequest
				AdapterInstanceId: id.String(),
				RequestType:       requesttype.Remove,
			},
		},
	}
	if err := uvm.Modify(&request); err != nil {
		return err
	}
	return nil
}
