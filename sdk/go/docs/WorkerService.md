# WorkerService

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Pool** | Pointer to **map[string]interface{}** |  | [optional] 

## Methods

### NewWorkerService

`func NewWorkerService() *WorkerService`

NewWorkerService instantiates a new WorkerService object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewWorkerServiceWithDefaults

`func NewWorkerServiceWithDefaults() *WorkerService`

NewWorkerServiceWithDefaults instantiates a new WorkerService object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetPool

`func (o *WorkerService) GetPool() map[string]interface{}`

GetPool returns the Pool field if non-nil, zero value otherwise.

### GetPoolOk

`func (o *WorkerService) GetPoolOk() (*map[string]interface{}, bool)`

GetPoolOk returns a tuple with the Pool field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPool

`func (o *WorkerService) SetPool(v map[string]interface{})`

SetPool sets Pool field to given value.

### HasPool

`func (o *WorkerService) HasPool() bool`

HasPool returns a boolean if a field has been set.

### SetPoolNil

`func (o *WorkerService) SetPoolNil(b bool)`

 SetPoolNil sets the value for Pool to be an explicit nil

### UnsetPool
`func (o *WorkerService) UnsetPool()`

UnsetPool ensures that no value is present for Pool, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


