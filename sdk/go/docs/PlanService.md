# PlanService

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Manager** | Pointer to **map[string]interface{}** |  | [optional] 
**Store** | Pointer to **map[string]interface{}** |  | [optional] 

## Methods

### NewPlanService

`func NewPlanService() *PlanService`

NewPlanService instantiates a new PlanService object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewPlanServiceWithDefaults

`func NewPlanServiceWithDefaults() *PlanService`

NewPlanServiceWithDefaults instantiates a new PlanService object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetManager

`func (o *PlanService) GetManager() map[string]interface{}`

GetManager returns the Manager field if non-nil, zero value otherwise.

### GetManagerOk

`func (o *PlanService) GetManagerOk() (*map[string]interface{}, bool)`

GetManagerOk returns a tuple with the Manager field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetManager

`func (o *PlanService) SetManager(v map[string]interface{})`

SetManager sets Manager field to given value.

### HasManager

`func (o *PlanService) HasManager() bool`

HasManager returns a boolean if a field has been set.

### SetManagerNil

`func (o *PlanService) SetManagerNil(b bool)`

 SetManagerNil sets the value for Manager to be an explicit nil

### UnsetManager
`func (o *PlanService) UnsetManager()`

UnsetManager ensures that no value is present for Manager, not even an explicit nil
### GetStore

`func (o *PlanService) GetStore() map[string]interface{}`

GetStore returns the Store field if non-nil, zero value otherwise.

### GetStoreOk

`func (o *PlanService) GetStoreOk() (*map[string]interface{}, bool)`

GetStoreOk returns a tuple with the Store field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStore

`func (o *PlanService) SetStore(v map[string]interface{})`

SetStore sets Store field to given value.

### HasStore

`func (o *PlanService) HasStore() bool`

HasStore returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


