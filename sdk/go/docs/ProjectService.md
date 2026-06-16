# ProjectService

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Pm** | Pointer to **map[string]interface{}** |  | [optional] 
**Store** | Pointer to **map[string]interface{}** |  | [optional] 

## Methods

### NewProjectService

`func NewProjectService() *ProjectService`

NewProjectService instantiates a new ProjectService object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewProjectServiceWithDefaults

`func NewProjectServiceWithDefaults() *ProjectService`

NewProjectServiceWithDefaults instantiates a new ProjectService object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetPm

`func (o *ProjectService) GetPm() map[string]interface{}`

GetPm returns the Pm field if non-nil, zero value otherwise.

### GetPmOk

`func (o *ProjectService) GetPmOk() (*map[string]interface{}, bool)`

GetPmOk returns a tuple with the Pm field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPm

`func (o *ProjectService) SetPm(v map[string]interface{})`

SetPm sets Pm field to given value.

### HasPm

`func (o *ProjectService) HasPm() bool`

HasPm returns a boolean if a field has been set.

### SetPmNil

`func (o *ProjectService) SetPmNil(b bool)`

 SetPmNil sets the value for Pm to be an explicit nil

### UnsetPm
`func (o *ProjectService) UnsetPm()`

UnsetPm ensures that no value is present for Pm, not even an explicit nil
### GetStore

`func (o *ProjectService) GetStore() map[string]interface{}`

GetStore returns the Store field if non-nil, zero value otherwise.

### GetStoreOk

`func (o *ProjectService) GetStoreOk() (*map[string]interface{}, bool)`

GetStoreOk returns a tuple with the Store field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStore

`func (o *ProjectService) SetStore(v map[string]interface{})`

SetStore sets Store field to given value.

### HasStore

`func (o *ProjectService) HasStore() bool`

HasStore returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


