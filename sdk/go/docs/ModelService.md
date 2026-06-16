# ModelService

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**ConfigPath** | Pointer to **string** |  | [optional] 
**CredStore** | Pointer to **map[string]interface{}** |  | [optional] 
**StateDir** | Pointer to **string** |  | [optional] 

## Methods

### NewModelService

`func NewModelService() *ModelService`

NewModelService instantiates a new ModelService object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewModelServiceWithDefaults

`func NewModelServiceWithDefaults() *ModelService`

NewModelServiceWithDefaults instantiates a new ModelService object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetConfigPath

`func (o *ModelService) GetConfigPath() string`

GetConfigPath returns the ConfigPath field if non-nil, zero value otherwise.

### GetConfigPathOk

`func (o *ModelService) GetConfigPathOk() (*string, bool)`

GetConfigPathOk returns a tuple with the ConfigPath field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetConfigPath

`func (o *ModelService) SetConfigPath(v string)`

SetConfigPath sets ConfigPath field to given value.

### HasConfigPath

`func (o *ModelService) HasConfigPath() bool`

HasConfigPath returns a boolean if a field has been set.

### GetCredStore

`func (o *ModelService) GetCredStore() map[string]interface{}`

GetCredStore returns the CredStore field if non-nil, zero value otherwise.

### GetCredStoreOk

`func (o *ModelService) GetCredStoreOk() (*map[string]interface{}, bool)`

GetCredStoreOk returns a tuple with the CredStore field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetCredStore

`func (o *ModelService) SetCredStore(v map[string]interface{})`

SetCredStore sets CredStore field to given value.

### HasCredStore

`func (o *ModelService) HasCredStore() bool`

HasCredStore returns a boolean if a field has been set.

### SetCredStoreNil

`func (o *ModelService) SetCredStoreNil(b bool)`

 SetCredStoreNil sets the value for CredStore to be an explicit nil

### UnsetCredStore
`func (o *ModelService) UnsetCredStore()`

UnsetCredStore ensures that no value is present for CredStore, not even an explicit nil
### GetStateDir

`func (o *ModelService) GetStateDir() string`

GetStateDir returns the StateDir field if non-nil, zero value otherwise.

### GetStateDirOk

`func (o *ModelService) GetStateDirOk() (*string, bool)`

GetStateDirOk returns a tuple with the StateDir field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStateDir

`func (o *ModelService) SetStateDir(v string)`

SetStateDir sets StateDir field to given value.

### HasStateDir

`func (o *ModelService) HasStateDir() bool`

HasStateDir returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


