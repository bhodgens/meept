# TaskService

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Registry** | Pointer to **map[string]interface{}** |  | [optional] 

## Methods

### NewTaskService

`func NewTaskService() *TaskService`

NewTaskService instantiates a new TaskService object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewTaskServiceWithDefaults

`func NewTaskServiceWithDefaults() *TaskService`

NewTaskServiceWithDefaults instantiates a new TaskService object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetRegistry

`func (o *TaskService) GetRegistry() map[string]interface{}`

GetRegistry returns the Registry field if non-nil, zero value otherwise.

### GetRegistryOk

`func (o *TaskService) GetRegistryOk() (*map[string]interface{}, bool)`

GetRegistryOk returns a tuple with the Registry field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetRegistry

`func (o *TaskService) SetRegistry(v map[string]interface{})`

SetRegistry sets Registry field to given value.

### HasRegistry

`func (o *TaskService) HasRegistry() bool`

HasRegistry returns a boolean if a field has been set.

### SetRegistryNil

`func (o *TaskService) SetRegistryNil(b bool)`

 SetRegistryNil sets the value for Registry to be an explicit nil

### UnsetRegistry
`func (o *TaskService) UnsetRegistry()`

UnsetRegistry ensures that no value is present for Registry, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


