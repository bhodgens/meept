# MemoryService

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Manager** | Pointer to **map[string]interface{}** |  | [optional] 

## Methods

### NewMemoryService

`func NewMemoryService() *MemoryService`

NewMemoryService instantiates a new MemoryService object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewMemoryServiceWithDefaults

`func NewMemoryServiceWithDefaults() *MemoryService`

NewMemoryServiceWithDefaults instantiates a new MemoryService object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetManager

`func (o *MemoryService) GetManager() map[string]interface{}`

GetManager returns the Manager field if non-nil, zero value otherwise.

### GetManagerOk

`func (o *MemoryService) GetManagerOk() (*map[string]interface{}, bool)`

GetManagerOk returns a tuple with the Manager field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetManager

`func (o *MemoryService) SetManager(v map[string]interface{})`

SetManager sets Manager field to given value.

### HasManager

`func (o *MemoryService) HasManager() bool`

HasManager returns a boolean if a field has been set.

### SetManagerNil

`func (o *MemoryService) SetManagerNil(b bool)`

 SetManagerNil sets the value for Manager to be an explicit nil

### UnsetManager
`func (o *MemoryService) UnsetManager()`

UnsetManager ensures that no value is present for Manager, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


