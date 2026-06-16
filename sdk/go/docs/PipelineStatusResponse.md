# PipelineStatusResponse

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**PipelineId** | **string** |  | 
**Name** | **string** |  | 
**Status** | **string** |  | 
**Steps** | **[]string** |  | 
**CreatedAt** | **string** |  | 
**UpdatedAt** | **string** |  | 

## Methods

### NewPipelineStatusResponse

`func NewPipelineStatusResponse(pipelineId string, name string, status string, steps []string, createdAt string, updatedAt string, ) *PipelineStatusResponse`

NewPipelineStatusResponse instantiates a new PipelineStatusResponse object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewPipelineStatusResponseWithDefaults

`func NewPipelineStatusResponseWithDefaults() *PipelineStatusResponse`

NewPipelineStatusResponseWithDefaults instantiates a new PipelineStatusResponse object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetPipelineId

`func (o *PipelineStatusResponse) GetPipelineId() string`

GetPipelineId returns the PipelineId field if non-nil, zero value otherwise.

### GetPipelineIdOk

`func (o *PipelineStatusResponse) GetPipelineIdOk() (*string, bool)`

GetPipelineIdOk returns a tuple with the PipelineId field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPipelineId

`func (o *PipelineStatusResponse) SetPipelineId(v string)`

SetPipelineId sets PipelineId field to given value.


### GetName

`func (o *PipelineStatusResponse) GetName() string`

GetName returns the Name field if non-nil, zero value otherwise.

### GetNameOk

`func (o *PipelineStatusResponse) GetNameOk() (*string, bool)`

GetNameOk returns a tuple with the Name field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetName

`func (o *PipelineStatusResponse) SetName(v string)`

SetName sets Name field to given value.


### GetStatus

`func (o *PipelineStatusResponse) GetStatus() string`

GetStatus returns the Status field if non-nil, zero value otherwise.

### GetStatusOk

`func (o *PipelineStatusResponse) GetStatusOk() (*string, bool)`

GetStatusOk returns a tuple with the Status field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStatus

`func (o *PipelineStatusResponse) SetStatus(v string)`

SetStatus sets Status field to given value.


### GetSteps

`func (o *PipelineStatusResponse) GetSteps() []string`

GetSteps returns the Steps field if non-nil, zero value otherwise.

### GetStepsOk

`func (o *PipelineStatusResponse) GetStepsOk() (*[]string, bool)`

GetStepsOk returns a tuple with the Steps field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSteps

`func (o *PipelineStatusResponse) SetSteps(v []string)`

SetSteps sets Steps field to given value.


### SetStepsNil

`func (o *PipelineStatusResponse) SetStepsNil(b bool)`

 SetStepsNil sets the value for Steps to be an explicit nil

### UnsetSteps
`func (o *PipelineStatusResponse) UnsetSteps()`

UnsetSteps ensures that no value is present for Steps, not even an explicit nil
### GetCreatedAt

`func (o *PipelineStatusResponse) GetCreatedAt() string`

GetCreatedAt returns the CreatedAt field if non-nil, zero value otherwise.

### GetCreatedAtOk

`func (o *PipelineStatusResponse) GetCreatedAtOk() (*string, bool)`

GetCreatedAtOk returns a tuple with the CreatedAt field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetCreatedAt

`func (o *PipelineStatusResponse) SetCreatedAt(v string)`

SetCreatedAt sets CreatedAt field to given value.


### GetUpdatedAt

`func (o *PipelineStatusResponse) GetUpdatedAt() string`

GetUpdatedAt returns the UpdatedAt field if non-nil, zero value otherwise.

### GetUpdatedAtOk

`func (o *PipelineStatusResponse) GetUpdatedAtOk() (*string, bool)`

GetUpdatedAtOk returns a tuple with the UpdatedAt field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetUpdatedAt

`func (o *PipelineStatusResponse) SetUpdatedAt(v string)`

SetUpdatedAt sets UpdatedAt field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


