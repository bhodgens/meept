# AddJobRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Id** | **string** |  | 
**Name** | **string** |  | 
**Schedule** | **string** |  | 
**Type** | **string** |  | 
**AgentConfigomitempty** | Pointer to **map[string]interface{}** |  | [optional] 
**ShellConfigomitempty** | Pointer to **map[string]interface{}** |  | [optional] 
**Enabledomitempty** | Pointer to **bool** |  | [optional] 

## Methods

### NewAddJobRequest

`func NewAddJobRequest(id string, name string, schedule string, type_ string, ) *AddJobRequest`

NewAddJobRequest instantiates a new AddJobRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewAddJobRequestWithDefaults

`func NewAddJobRequestWithDefaults() *AddJobRequest`

NewAddJobRequestWithDefaults instantiates a new AddJobRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetId

`func (o *AddJobRequest) GetId() string`

GetId returns the Id field if non-nil, zero value otherwise.

### GetIdOk

`func (o *AddJobRequest) GetIdOk() (*string, bool)`

GetIdOk returns a tuple with the Id field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetId

`func (o *AddJobRequest) SetId(v string)`

SetId sets Id field to given value.


### GetName

`func (o *AddJobRequest) GetName() string`

GetName returns the Name field if non-nil, zero value otherwise.

### GetNameOk

`func (o *AddJobRequest) GetNameOk() (*string, bool)`

GetNameOk returns a tuple with the Name field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetName

`func (o *AddJobRequest) SetName(v string)`

SetName sets Name field to given value.


### GetSchedule

`func (o *AddJobRequest) GetSchedule() string`

GetSchedule returns the Schedule field if non-nil, zero value otherwise.

### GetScheduleOk

`func (o *AddJobRequest) GetScheduleOk() (*string, bool)`

GetScheduleOk returns a tuple with the Schedule field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSchedule

`func (o *AddJobRequest) SetSchedule(v string)`

SetSchedule sets Schedule field to given value.


### GetType

`func (o *AddJobRequest) GetType() string`

GetType returns the Type field if non-nil, zero value otherwise.

### GetTypeOk

`func (o *AddJobRequest) GetTypeOk() (*string, bool)`

GetTypeOk returns a tuple with the Type field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetType

`func (o *AddJobRequest) SetType(v string)`

SetType sets Type field to given value.


### GetAgentConfigomitempty

`func (o *AddJobRequest) GetAgentConfigomitempty() map[string]interface{}`

GetAgentConfigomitempty returns the AgentConfigomitempty field if non-nil, zero value otherwise.

### GetAgentConfigomitemptyOk

`func (o *AddJobRequest) GetAgentConfigomitemptyOk() (*map[string]interface{}, bool)`

GetAgentConfigomitemptyOk returns a tuple with the AgentConfigomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetAgentConfigomitempty

`func (o *AddJobRequest) SetAgentConfigomitempty(v map[string]interface{})`

SetAgentConfigomitempty sets AgentConfigomitempty field to given value.

### HasAgentConfigomitempty

`func (o *AddJobRequest) HasAgentConfigomitempty() bool`

HasAgentConfigomitempty returns a boolean if a field has been set.

### SetAgentConfigomitemptyNil

`func (o *AddJobRequest) SetAgentConfigomitemptyNil(b bool)`

 SetAgentConfigomitemptyNil sets the value for AgentConfigomitempty to be an explicit nil

### UnsetAgentConfigomitempty
`func (o *AddJobRequest) UnsetAgentConfigomitempty()`

UnsetAgentConfigomitempty ensures that no value is present for AgentConfigomitempty, not even an explicit nil
### GetShellConfigomitempty

`func (o *AddJobRequest) GetShellConfigomitempty() map[string]interface{}`

GetShellConfigomitempty returns the ShellConfigomitempty field if non-nil, zero value otherwise.

### GetShellConfigomitemptyOk

`func (o *AddJobRequest) GetShellConfigomitemptyOk() (*map[string]interface{}, bool)`

GetShellConfigomitemptyOk returns a tuple with the ShellConfigomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetShellConfigomitempty

`func (o *AddJobRequest) SetShellConfigomitempty(v map[string]interface{})`

SetShellConfigomitempty sets ShellConfigomitempty field to given value.

### HasShellConfigomitempty

`func (o *AddJobRequest) HasShellConfigomitempty() bool`

HasShellConfigomitempty returns a boolean if a field has been set.

### SetShellConfigomitemptyNil

`func (o *AddJobRequest) SetShellConfigomitemptyNil(b bool)`

 SetShellConfigomitemptyNil sets the value for ShellConfigomitempty to be an explicit nil

### UnsetShellConfigomitempty
`func (o *AddJobRequest) UnsetShellConfigomitempty()`

UnsetShellConfigomitempty ensures that no value is present for ShellConfigomitempty, not even an explicit nil
### GetEnabledomitempty

`func (o *AddJobRequest) GetEnabledomitempty() bool`

GetEnabledomitempty returns the Enabledomitempty field if non-nil, zero value otherwise.

### GetEnabledomitemptyOk

`func (o *AddJobRequest) GetEnabledomitemptyOk() (*bool, bool)`

GetEnabledomitemptyOk returns a tuple with the Enabledomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetEnabledomitempty

`func (o *AddJobRequest) SetEnabledomitempty(v bool)`

SetEnabledomitempty sets Enabledomitempty field to given value.

### HasEnabledomitempty

`func (o *AddJobRequest) HasEnabledomitempty() bool`

HasEnabledomitempty returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


