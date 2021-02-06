package upcloud

import (
	"fmt"
	"strings"

	"github.com/UpCloudLtd/upcloud-go-api/upcloud"
	"github.com/UpCloudLtd/upcloud-go-api/upcloud/client"
	"github.com/UpCloudLtd/upcloud-go-api/upcloud/request"
	"github.com/UpCloudLtd/upcloud-go-api/upcloud/service"
)

type (
	Driver interface {
		CreateServer(string, string) (*upcloud.ServerDetails, error)
		DeleteServer(string) error
		StopServer(string) error
		CreateTemplate(string) error
		GetTemplate() (*upcloud.Storage, error)
	}

	driver struct {
		svc    *service.Service
		config *Config
	}
)

func NewDriver(c *Config) Driver {
	client := client.New(c.Username, c.Password)
	svc := service.New(client)
	return &driver{
		svc:    svc,
		config: c,
	}
}

func (d *driver) CreateServer(templateUuid, sshKeyPublic string) (*upcloud.ServerDetails, error) {
	// Create server
	request := d.prepareCreateRequest(templateUuid, sshKeyPublic)
	response, err := d.svc.CreateServer(request)
	if err != nil {
		return nil, fmt.Errorf("Error creating server: %s", err)
	}

	// Wait for server to start
	err = d.waitDesiredState(response.UUID, upcloud.ServerStateStarted)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (d *driver) DeleteServer(serverUuid string) error {
	// get storage to delete it once server deleted
	storage, err := d.getServerStorage(serverUuid)
	if err != nil {
		return err
	}

	// delete server
	err = d.svc.DeleteServer(&request.DeleteServerRequest{
		UUID: serverUuid,
	})
	if err != nil {
		return fmt.Errorf("Failed to delete server: %s", err)
	}

	// delete storage
	err = d.svc.DeleteStorage(&request.DeleteStorageRequest{
		UUID: storage.UUID,
	})
	return nil
}

func (d *driver) StopServer(serverUuid string) error {
	// Ensure the instance is not in maintenance state
	err := d.waitUndesiredState(serverUuid, upcloud.ServerStateMaintenance)
	if err != nil {
		return err
	}

	// Check current server state and do nothing if already stopped
	response, err := d.getServerDetails(serverUuid)
	if err != nil {
		return err
	}

	if response.State == upcloud.ServerStateStopped {
		return nil
	}

	// Stop server
	_, err = d.svc.StopServer(&request.StopServerRequest{
		UUID: serverUuid,
	})
	if err != nil {
		return fmt.Errorf("Failed to stop server: %s", err)
	}

	// Wait for server to stop
	err = d.waitDesiredState(serverUuid, upcloud.ServerStateStopped)
	if err != nil {
		return err
	}
	return nil
}

func (d *driver) CreateTemplate(serverUuid string) error {
	// get storage details
	storage, err := d.getServerStorage(serverUuid)
	if err != nil {
		return err
	}

	// create image
	imageTitle := fmt.Sprintf("%s-%s", d.config.ImageName, GetNowString())
	response, err := d.svc.TemplatizeStorage(&request.TemplatizeStorageRequest{
		UUID:  storage.UUID,
		Title: imageTitle,
	})
	if err != nil {
		return fmt.Errorf("Error creating image: %s", err)
	}

	// wait for online state
	_, err = d.svc.WaitForStorageState(&request.WaitForStorageStateRequest{
		UUID:         response.UUID,
		DesiredState: upcloud.StorageStateOnline,
		Timeout:      d.config.Timeout,
	})
	if err != nil {
		return fmt.Errorf("Error while waiting for storage to change state to 'online': %s", err)
	}
	return nil
}

func (d *driver) GetTemplate() (*upcloud.Storage, error) {
	templateUuid := d.config.TemplateUUID
	templateName := d.config.TemplateName

	if templateUuid != "" {
		template, err := d.getTemplateByUuid(templateUuid)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving template by uuid %q: %s", templateUuid, err)
		}
		return template, nil
	}

	if templateName != "" {
		template, err := d.getTemplateByName(templateName)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving template by name %q: %s", templateName, err)
		}
		return template, nil

	}
	return nil, fmt.Errorf("Error retrieving template")
}

func (d *driver) getTemplateByUuid(templateUuid string) (*upcloud.Storage, error) {
	request := &request.GetStorageDetailsRequest{
		UUID: templateUuid,
	}
	response, err := d.svc.GetStorageDetails(request)

	if err != nil {
		return nil, fmt.Errorf("Error fetching templates: %s", err)
	}

	return &response.Storage, nil
}

func (d *driver) getTemplateByName(templateName string) (*upcloud.Storage, error) {
	request := &request.GetStoragesRequest{
		Type: upcloud.StorageTypeTemplate,
	}
	response, err := d.svc.GetStorages(request)

	if err != nil {
		return nil, fmt.Errorf("Error fetching templates: %s", err)
	}

	var found bool
	var template upcloud.Storage
	for _, t := range response.Storages {
		if strings.Contains(strings.ToLower(t.Title), strings.ToLower(templateName)) {
			found = true
			template = t
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("Failed to find template by name %q", templateName)
	}
	return &template, nil
}

func (d *driver) waitDesiredState(serverUuid string, state string) error {
	request := &request.WaitForServerStateRequest{
		UUID:         serverUuid,
		DesiredState: state,
		Timeout:      d.config.Timeout,
	}
	if _, err := d.svc.WaitForServerState(request); err != nil {
		return fmt.Errorf("Error while waiting for server to change state to %q: %s", state, err)
	}
	return nil
}

func (d *driver) waitUndesiredState(serverUuid string, state string) error {
	request := &request.WaitForServerStateRequest{
		UUID:           serverUuid,
		UndesiredState: state,
		Timeout:        d.config.Timeout,
	}
	if _, err := d.svc.WaitForServerState(request); err != nil {
		return fmt.Errorf("Error while waiting for server to change state from %q: %s", state, err)
	}
	return nil
}

func (d *driver) getServerDetails(serverUuid string) (*upcloud.ServerDetails, error) {
	response, err := d.svc.GetServerDetails(&request.GetServerDetailsRequest{
		UUID: serverUuid,
	})
	if err != nil {
		return nil, fmt.Errorf("Failed to get details for server: %s", err)
	}
	return response, nil
}

func (d *driver) getServerStorage(serverUuid string) (*upcloud.ServerStorageDevice, error) {
	details, err := d.getServerDetails(serverUuid)
	if err != nil {
		return nil, err
	}

	var found bool
	var storage upcloud.ServerStorageDevice
	for _, s := range details.StorageDevices {
		if s.Type == upcloud.StorageTypeDisk {
			found = true
			storage = s
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("Failed to find storage type disk for server %q", serverUuid)
	}
	return &storage, nil
}

func (d *driver) prepareCreateRequest(templateUuid, sshKeyPublic string) *request.CreateServerRequest {
	title := fmt.Sprintf("packer-%s-%s", d.config.ImageName, GetNowString())
	hostname := d.config.ImageName
	titleDisk := fmt.Sprintf("%s-disk1", title)

	return &request.CreateServerRequest{
		Title:            title,
		Hostname:         hostname,
		Zone:             d.config.Zone,
		PasswordDelivery: request.PasswordDeliveryNone,
		CoreNumber:       2,
		MemoryAmount:     2048,
		StorageDevices: []request.CreateServerStorageDevice{
			{
				Action:  request.CreateServerStorageDeviceActionClone,
				Storage: templateUuid,
				Title:   titleDisk,
				Size:    d.config.StorageSize,
				Tier:    upcloud.StorageTierMaxIOPS,
			},
		},
		Networking: &request.CreateServerNetworking{
			Interfaces: []request.CreateServerInterface{
				{
					IPAddresses: []request.CreateServerIPAddress{
						{
							Family: upcloud.IPAddressFamilyIPv4,
						},
					},
					Type: upcloud.IPAddressAccessPublic,
				},
				{
					IPAddresses: []request.CreateServerIPAddress{
						{
							Family: upcloud.IPAddressFamilyIPv4,
						},
					},
					Type: upcloud.IPAddressAccessUtility,
				},
				{
					IPAddresses: []request.CreateServerIPAddress{
						{
							Family: upcloud.IPAddressFamilyIPv6,
						},
					},
					Type: upcloud.IPAddressAccessPublic,
				},
			},
		},
		LoginUser: &request.LoginUser{
			CreatePassword: "no",
			Username:       d.config.Comm.SSHUsername,
			SSHKeys: []string{
				sshKeyPublic,
			},
		},
	}
}
