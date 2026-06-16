# UpdateStatusRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**PipelineId** | **string** |  | 
**Status** | **string** |  | 

## Methods

### NewUpdateStatusRequest

`func NewUpdateStatusRequest(pipelineId string, status string, ) *UpdateStatusRequest`

NewUpdateStatusRequest instantiates a new UpdateStatusRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewUpdateStatusRequestWithDefaults

`func NewUpdateStatusRequestWithDefaults() *UpdateStatusRequest`

NewUpdateStatusRequestWithDefaults instantiates a new UpdateStatusRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetPipelineId

`func (o *UpdateStatusRequest) GetPipelineId() string`

GetPipelineId returns the PipelineId field if non-nil, zero value otherwise.

### GetPipelineIdOk

`func (o *UpdateStatusRequest) GetPipelineIdOk() (*string, bool)`

GetPipelineIdOk returns a tuple with the PipelineId field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPipelineId

`func (o *UpdateStatusRequest) SetPipelineId(v string)`

SetPipelineId sets PipelineId field to given value.


### GetStatus

`func (o *UpdateStatusRequest) GetStatus() string`

GetStatus returns the Status field if non-nil, zero value otherwise.

### GetStatusOk

`func (o *UpdateStatusRequest) GetStatusOk() (*string, bool)`

GetStatusOk returns a tuple with the Status field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStatus

`func (o *UpdateStatusRequest) SetStatus(v string)`

SetStatus sets Status field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


