# SearchService

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**SessionStore** | Pointer to **map[string]interface{}** |  | [optional] 
**TaskRegistry** | Pointer to **map[string]interface{}** |  | [optional] 
**MemoryMgr** | Pointer to **map[string]interface{}** |  | [optional] 
**PlanStore** | Pointer to **map[string]interface{}** |  | [optional] 

## Methods

### NewSearchService

`func NewSearchService() *SearchService`

NewSearchService instantiates a new SearchService object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewSearchServiceWithDefaults

`func NewSearchServiceWithDefaults() *SearchService`

NewSearchServiceWithDefaults instantiates a new SearchService object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetSessionStore

`func (o *SearchService) GetSessionStore() map[string]interface{}`

GetSessionStore returns the SessionStore field if non-nil, zero value otherwise.

### GetSessionStoreOk

`func (o *SearchService) GetSessionStoreOk() (*map[string]interface{}, bool)`

GetSessionStoreOk returns a tuple with the SessionStore field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSessionStore

`func (o *SearchService) SetSessionStore(v map[string]interface{})`

SetSessionStore sets SessionStore field to given value.

### HasSessionStore

`func (o *SearchService) HasSessionStore() bool`

HasSessionStore returns a boolean if a field has been set.

### GetTaskRegistry

`func (o *SearchService) GetTaskRegistry() map[string]interface{}`

GetTaskRegistry returns the TaskRegistry field if non-nil, zero value otherwise.

### GetTaskRegistryOk

`func (o *SearchService) GetTaskRegistryOk() (*map[string]interface{}, bool)`

GetTaskRegistryOk returns a tuple with the TaskRegistry field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTaskRegistry

`func (o *SearchService) SetTaskRegistry(v map[string]interface{})`

SetTaskRegistry sets TaskRegistry field to given value.

### HasTaskRegistry

`func (o *SearchService) HasTaskRegistry() bool`

HasTaskRegistry returns a boolean if a field has been set.

### SetTaskRegistryNil

`func (o *SearchService) SetTaskRegistryNil(b bool)`

 SetTaskRegistryNil sets the value for TaskRegistry to be an explicit nil

### UnsetTaskRegistry
`func (o *SearchService) UnsetTaskRegistry()`

UnsetTaskRegistry ensures that no value is present for TaskRegistry, not even an explicit nil
### GetMemoryMgr

`func (o *SearchService) GetMemoryMgr() map[string]interface{}`

GetMemoryMgr returns the MemoryMgr field if non-nil, zero value otherwise.

### GetMemoryMgrOk

`func (o *SearchService) GetMemoryMgrOk() (*map[string]interface{}, bool)`

GetMemoryMgrOk returns a tuple with the MemoryMgr field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetMemoryMgr

`func (o *SearchService) SetMemoryMgr(v map[string]interface{})`

SetMemoryMgr sets MemoryMgr field to given value.

### HasMemoryMgr

`func (o *SearchService) HasMemoryMgr() bool`

HasMemoryMgr returns a boolean if a field has been set.

### SetMemoryMgrNil

`func (o *SearchService) SetMemoryMgrNil(b bool)`

 SetMemoryMgrNil sets the value for MemoryMgr to be an explicit nil

### UnsetMemoryMgr
`func (o *SearchService) UnsetMemoryMgr()`

UnsetMemoryMgr ensures that no value is present for MemoryMgr, not even an explicit nil
### GetPlanStore

`func (o *SearchService) GetPlanStore() map[string]interface{}`

GetPlanStore returns the PlanStore field if non-nil, zero value otherwise.

### GetPlanStoreOk

`func (o *SearchService) GetPlanStoreOk() (*map[string]interface{}, bool)`

GetPlanStoreOk returns a tuple with the PlanStore field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPlanStore

`func (o *SearchService) SetPlanStore(v map[string]interface{})`

SetPlanStore sets PlanStore field to given value.

### HasPlanStore

`func (o *SearchService) HasPlanStore() bool`

HasPlanStore returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


