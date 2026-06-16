# Pipeline

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Id** | **string** |  | 
**Name** | **string** |  | 
**Description** | **string** |  | 
**Status** | **string** |  | 
**Steps** | **[]string** |  | 
**Metadata** | **NullableString** |  | 
**CreatedAt** | **string** |  | 
**UpdatedAt** | **string** |  | 

## Methods

### NewPipeline

`func NewPipeline(id string, name string, description string, status string, steps []string, metadata NullableString, createdAt string, updatedAt string, ) *Pipeline`

NewPipeline instantiates a new Pipeline object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewPipelineWithDefaults

`func NewPipelineWithDefaults() *Pipeline`

NewPipelineWithDefaults instantiates a new Pipeline object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetId

`func (o *Pipeline) GetId() string`

GetId returns the Id field if non-nil, zero value otherwise.

### GetIdOk

`func (o *Pipeline) GetIdOk() (*string, bool)`

GetIdOk returns a tuple with the Id field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetId

`func (o *Pipeline) SetId(v string)`

SetId sets Id field to given value.


### GetName

`func (o *Pipeline) GetName() string`

GetName returns the Name field if non-nil, zero value otherwise.

### GetNameOk

`func (o *Pipeline) GetNameOk() (*string, bool)`

GetNameOk returns a tuple with the Name field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetName

`func (o *Pipeline) SetName(v string)`

SetName sets Name field to given value.


### GetDescription

`func (o *Pipeline) GetDescription() string`

GetDescription returns the Description field if non-nil, zero value otherwise.

### GetDescriptionOk

`func (o *Pipeline) GetDescriptionOk() (*string, bool)`

GetDescriptionOk returns a tuple with the Description field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetDescription

`func (o *Pipeline) SetDescription(v string)`

SetDescription sets Description field to given value.


### GetStatus

`func (o *Pipeline) GetStatus() string`

GetStatus returns the Status field if non-nil, zero value otherwise.

### GetStatusOk

`func (o *Pipeline) GetStatusOk() (*string, bool)`

GetStatusOk returns a tuple with the Status field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStatus

`func (o *Pipeline) SetStatus(v string)`

SetStatus sets Status field to given value.


### GetSteps

`func (o *Pipeline) GetSteps() []string`

GetSteps returns the Steps field if non-nil, zero value otherwise.

### GetStepsOk

`func (o *Pipeline) GetStepsOk() (*[]string, bool)`

GetStepsOk returns a tuple with the Steps field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSteps

`func (o *Pipeline) SetSteps(v []string)`

SetSteps sets Steps field to given value.


### SetStepsNil

`func (o *Pipeline) SetStepsNil(b bool)`

 SetStepsNil sets the value for Steps to be an explicit nil

### UnsetSteps
`func (o *Pipeline) UnsetSteps()`

UnsetSteps ensures that no value is present for Steps, not even an explicit nil
### GetMetadata

`func (o *Pipeline) GetMetadata() string`

GetMetadata returns the Metadata field if non-nil, zero value otherwise.

### GetMetadataOk

`func (o *Pipeline) GetMetadataOk() (*string, bool)`

GetMetadataOk returns a tuple with the Metadata field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetMetadata

`func (o *Pipeline) SetMetadata(v string)`

SetMetadata sets Metadata field to given value.


### SetMetadataNil

`func (o *Pipeline) SetMetadataNil(b bool)`

 SetMetadataNil sets the value for Metadata to be an explicit nil

### UnsetMetadata
`func (o *Pipeline) UnsetMetadata()`

UnsetMetadata ensures that no value is present for Metadata, not even an explicit nil
### GetCreatedAt

`func (o *Pipeline) GetCreatedAt() string`

GetCreatedAt returns the CreatedAt field if non-nil, zero value otherwise.

### GetCreatedAtOk

`func (o *Pipeline) GetCreatedAtOk() (*string, bool)`

GetCreatedAtOk returns a tuple with the CreatedAt field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetCreatedAt

`func (o *Pipeline) SetCreatedAt(v string)`

SetCreatedAt sets CreatedAt field to given value.


### GetUpdatedAt

`func (o *Pipeline) GetUpdatedAt() string`

GetUpdatedAt returns the UpdatedAt field if non-nil, zero value otherwise.

### GetUpdatedAtOk

`func (o *Pipeline) GetUpdatedAtOk() (*string, bool)`

GetUpdatedAtOk returns a tuple with the UpdatedAt field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetUpdatedAt

`func (o *Pipeline) SetUpdatedAt(v string)`

SetUpdatedAt sets UpdatedAt field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


