# PipelineStepStatus

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Id** | **string** |  | 
**Name** | **string** |  | 
**Status** | **string** |  | 
**Erroromitempty** | Pointer to **string** |  | [optional] 
**StartedAtomitempty** | Pointer to **NullableString** |  | [optional] 
**EndedAtomitempty** | Pointer to **NullableString** |  | [optional] 

## Methods

### NewPipelineStepStatus

`func NewPipelineStepStatus(id string, name string, status string, ) *PipelineStepStatus`

NewPipelineStepStatus instantiates a new PipelineStepStatus object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewPipelineStepStatusWithDefaults

`func NewPipelineStepStatusWithDefaults() *PipelineStepStatus`

NewPipelineStepStatusWithDefaults instantiates a new PipelineStepStatus object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetId

`func (o *PipelineStepStatus) GetId() string`

GetId returns the Id field if non-nil, zero value otherwise.

### GetIdOk

`func (o *PipelineStepStatus) GetIdOk() (*string, bool)`

GetIdOk returns a tuple with the Id field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetId

`func (o *PipelineStepStatus) SetId(v string)`

SetId sets Id field to given value.


### GetName

`func (o *PipelineStepStatus) GetName() string`

GetName returns the Name field if non-nil, zero value otherwise.

### GetNameOk

`func (o *PipelineStepStatus) GetNameOk() (*string, bool)`

GetNameOk returns a tuple with the Name field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetName

`func (o *PipelineStepStatus) SetName(v string)`

SetName sets Name field to given value.


### GetStatus

`func (o *PipelineStepStatus) GetStatus() string`

GetStatus returns the Status field if non-nil, zero value otherwise.

### GetStatusOk

`func (o *PipelineStepStatus) GetStatusOk() (*string, bool)`

GetStatusOk returns a tuple with the Status field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStatus

`func (o *PipelineStepStatus) SetStatus(v string)`

SetStatus sets Status field to given value.


### GetErroromitempty

`func (o *PipelineStepStatus) GetErroromitempty() string`

GetErroromitempty returns the Erroromitempty field if non-nil, zero value otherwise.

### GetErroromitemptyOk

`func (o *PipelineStepStatus) GetErroromitemptyOk() (*string, bool)`

GetErroromitemptyOk returns a tuple with the Erroromitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetErroromitempty

`func (o *PipelineStepStatus) SetErroromitempty(v string)`

SetErroromitempty sets Erroromitempty field to given value.

### HasErroromitempty

`func (o *PipelineStepStatus) HasErroromitempty() bool`

HasErroromitempty returns a boolean if a field has been set.

### GetStartedAtomitempty

`func (o *PipelineStepStatus) GetStartedAtomitempty() string`

GetStartedAtomitempty returns the StartedAtomitempty field if non-nil, zero value otherwise.

### GetStartedAtomitemptyOk

`func (o *PipelineStepStatus) GetStartedAtomitemptyOk() (*string, bool)`

GetStartedAtomitemptyOk returns a tuple with the StartedAtomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStartedAtomitempty

`func (o *PipelineStepStatus) SetStartedAtomitempty(v string)`

SetStartedAtomitempty sets StartedAtomitempty field to given value.

### HasStartedAtomitempty

`func (o *PipelineStepStatus) HasStartedAtomitempty() bool`

HasStartedAtomitempty returns a boolean if a field has been set.

### SetStartedAtomitemptyNil

`func (o *PipelineStepStatus) SetStartedAtomitemptyNil(b bool)`

 SetStartedAtomitemptyNil sets the value for StartedAtomitempty to be an explicit nil

### UnsetStartedAtomitempty
`func (o *PipelineStepStatus) UnsetStartedAtomitempty()`

UnsetStartedAtomitempty ensures that no value is present for StartedAtomitempty, not even an explicit nil
### GetEndedAtomitempty

`func (o *PipelineStepStatus) GetEndedAtomitempty() string`

GetEndedAtomitempty returns the EndedAtomitempty field if non-nil, zero value otherwise.

### GetEndedAtomitemptyOk

`func (o *PipelineStepStatus) GetEndedAtomitemptyOk() (*string, bool)`

GetEndedAtomitemptyOk returns a tuple with the EndedAtomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetEndedAtomitempty

`func (o *PipelineStepStatus) SetEndedAtomitempty(v string)`

SetEndedAtomitempty sets EndedAtomitempty field to given value.

### HasEndedAtomitempty

`func (o *PipelineStepStatus) HasEndedAtomitempty() bool`

HasEndedAtomitempty returns a boolean if a field has been set.

### SetEndedAtomitemptyNil

`func (o *PipelineStepStatus) SetEndedAtomitemptyNil(b bool)`

 SetEndedAtomitemptyNil sets the value for EndedAtomitempty to be an explicit nil

### UnsetEndedAtomitempty
`func (o *PipelineStepStatus) UnsetEndedAtomitempty()`

UnsetEndedAtomitempty ensures that no value is present for EndedAtomitempty, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


