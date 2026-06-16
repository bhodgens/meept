# DaemonService

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**PidFile** | Pointer to **string** |  | [optional] 
**StateDir** | Pointer to **string** |  | [optional] 
**BinPath** | Pointer to **string** |  | [optional] 
**Controller** | Pointer to **map[string]interface{}** |  | [optional] 

## Methods

### NewDaemonService

`func NewDaemonService() *DaemonService`

NewDaemonService instantiates a new DaemonService object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewDaemonServiceWithDefaults

`func NewDaemonServiceWithDefaults() *DaemonService`

NewDaemonServiceWithDefaults instantiates a new DaemonService object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetPidFile

`func (o *DaemonService) GetPidFile() string`

GetPidFile returns the PidFile field if non-nil, zero value otherwise.

### GetPidFileOk

`func (o *DaemonService) GetPidFileOk() (*string, bool)`

GetPidFileOk returns a tuple with the PidFile field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPidFile

`func (o *DaemonService) SetPidFile(v string)`

SetPidFile sets PidFile field to given value.

### HasPidFile

`func (o *DaemonService) HasPidFile() bool`

HasPidFile returns a boolean if a field has been set.

### GetStateDir

`func (o *DaemonService) GetStateDir() string`

GetStateDir returns the StateDir field if non-nil, zero value otherwise.

### GetStateDirOk

`func (o *DaemonService) GetStateDirOk() (*string, bool)`

GetStateDirOk returns a tuple with the StateDir field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStateDir

`func (o *DaemonService) SetStateDir(v string)`

SetStateDir sets StateDir field to given value.

### HasStateDir

`func (o *DaemonService) HasStateDir() bool`

HasStateDir returns a boolean if a field has been set.

### GetBinPath

`func (o *DaemonService) GetBinPath() string`

GetBinPath returns the BinPath field if non-nil, zero value otherwise.

### GetBinPathOk

`func (o *DaemonService) GetBinPathOk() (*string, bool)`

GetBinPathOk returns a tuple with the BinPath field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetBinPath

`func (o *DaemonService) SetBinPath(v string)`

SetBinPath sets BinPath field to given value.

### HasBinPath

`func (o *DaemonService) HasBinPath() bool`

HasBinPath returns a boolean if a field has been set.

### GetController

`func (o *DaemonService) GetController() map[string]interface{}`

GetController returns the Controller field if non-nil, zero value otherwise.

### GetControllerOk

`func (o *DaemonService) GetControllerOk() (*map[string]interface{}, bool)`

GetControllerOk returns a tuple with the Controller field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetController

`func (o *DaemonService) SetController(v map[string]interface{})`

SetController sets Controller field to given value.

### HasController

`func (o *DaemonService) HasController() bool`

HasController returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


