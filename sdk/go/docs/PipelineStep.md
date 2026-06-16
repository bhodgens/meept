# PipelineStep

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

### NewPipelineStep

`func NewPipelineStep(id string, name string, status string, ) *PipelineStep`

NewPipelineStep instantiates a new PipelineStep object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewPipelineStepWithDefaults

`func NewPipelineStepWithDefaults() *PipelineStep`

NewPipelineStepWithDefaults instantiates a new PipelineStep object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetId

`func (o *PipelineStep) GetId() string`

GetId returns the Id field if non-nil, zero value otherwise.

### GetIdOk

`func (o *PipelineStep) GetIdOk() (*string, bool)`

GetIdOk returns a tuple with the Id field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetId

`func (o *PipelineStep) SetId(v string)`

SetId sets Id field to given value.


### GetName

`func (o *PipelineStep) GetName() string`

GetName returns the Name field if non-nil, zero value otherwise.

### GetNameOk

`func (o *PipelineStep) GetNameOk() (*string, bool)`

GetNameOk returns a tuple with the Name field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetName

`func (o *PipelineStep) SetName(v string)`

SetName sets Name field to given value.


### GetStatus

`func (o *PipelineStep) GetStatus() string`

GetStatus returns the Status field if non-nil, zero value otherwise.

### GetStatusOk

`func (o *PipelineStep) GetStatusOk() (*string, bool)`

GetStatusOk returns a tuple with the Status field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStatus

`func (o *PipelineStep) SetStatus(v string)`

SetStatus sets Status field to given value.


### GetErroromitempty

`func (o *PipelineStep) GetErroromitempty() string`

GetErroromitempty returns the Erroromitempty field if non-nil, zero value otherwise.

### GetErroromitemptyOk

`func (o *PipelineStep) GetErroromitemptyOk() (*string, bool)`

GetErroromitemptyOk returns a tuple with the Erroromitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetErroromitempty

`func (o *PipelineStep) SetErroromitempty(v string)`

SetErroromitempty sets Erroromitempty field to given value.

### HasErroromitempty

`func (o *PipelineStep) HasErroromitempty() bool`

HasErroromitempty returns a boolean if a field has been set.

### GetStartedAtomitempty

`func (o *PipelineStep) GetStartedAtomitempty() string`

GetStartedAtomitempty returns the StartedAtomitempty field if non-nil, zero value otherwise.

### GetStartedAtomitemptyOk

`func (o *PipelineStep) GetStartedAtomitemptyOk() (*string, bool)`

GetStartedAtomitemptyOk returns a tuple with the StartedAtomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStartedAtomitempty

`func (o *PipelineStep) SetStartedAtomitempty(v string)`

SetStartedAtomitempty sets StartedAtomitempty field to given value.

### HasStartedAtomitempty

`func (o *PipelineStep) HasStartedAtomitempty() bool`

HasStartedAtomitempty returns a boolean if a field has been set.

### SetStartedAtomitemptyNil

`func (o *PipelineStep) SetStartedAtomitemptyNil(b bool)`

 SetStartedAtomitemptyNil sets the value for StartedAtomitempty to be an explicit nil

### UnsetStartedAtomitempty
`func (o *PipelineStep) UnsetStartedAtomitempty()`

UnsetStartedAtomitempty ensures that no value is present for StartedAtomitempty, not even an explicit nil
### GetEndedAtomitempty

`func (o *PipelineStep) GetEndedAtomitempty() string`

GetEndedAtomitempty returns the EndedAtomitempty field if non-nil, zero value otherwise.

### GetEndedAtomitemptyOk

`func (o *PipelineStep) GetEndedAtomitemptyOk() (*string, bool)`

GetEndedAtomitemptyOk returns a tuple with the EndedAtomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetEndedAtomitempty

`func (o *PipelineStep) SetEndedAtomitempty(v string)`

SetEndedAtomitempty sets EndedAtomitempty field to given value.

### HasEndedAtomitempty

`func (o *PipelineStep) HasEndedAtomitempty() bool`

HasEndedAtomitempty returns a boolean if a field has been set.

### SetEndedAtomitemptyNil

`func (o *PipelineStep) SetEndedAtomitemptyNil(b bool)`

 SetEndedAtomitemptyNil sets the value for EndedAtomitempty to be an explicit nil

### UnsetEndedAtomitempty
`func (o *PipelineStep) UnsetEndedAtomitempty()`

UnsetEndedAtomitempty ensures that no value is present for EndedAtomitempty, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


