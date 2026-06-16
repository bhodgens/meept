# PipelineService

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Mu** | Pointer to **map[string]interface{}** |  | [optional] 
**Pipelines** | Pointer to **NullableString** |  | [optional] 

## Methods

### NewPipelineService

`func NewPipelineService() *PipelineService`

NewPipelineService instantiates a new PipelineService object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewPipelineServiceWithDefaults

`func NewPipelineServiceWithDefaults() *PipelineService`

NewPipelineServiceWithDefaults instantiates a new PipelineService object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetMu

`func (o *PipelineService) GetMu() map[string]interface{}`

GetMu returns the Mu field if non-nil, zero value otherwise.

### GetMuOk

`func (o *PipelineService) GetMuOk() (*map[string]interface{}, bool)`

GetMuOk returns a tuple with the Mu field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetMu

`func (o *PipelineService) SetMu(v map[string]interface{})`

SetMu sets Mu field to given value.

### HasMu

`func (o *PipelineService) HasMu() bool`

HasMu returns a boolean if a field has been set.

### GetPipelines

`func (o *PipelineService) GetPipelines() string`

GetPipelines returns the Pipelines field if non-nil, zero value otherwise.

### GetPipelinesOk

`func (o *PipelineService) GetPipelinesOk() (*string, bool)`

GetPipelinesOk returns a tuple with the Pipelines field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPipelines

`func (o *PipelineService) SetPipelines(v string)`

SetPipelines sets Pipelines field to given value.

### HasPipelines

`func (o *PipelineService) HasPipelines() bool`

HasPipelines returns a boolean if a field has been set.

### SetPipelinesNil

`func (o *PipelineService) SetPipelinesNil(b bool)`

 SetPipelinesNil sets the value for Pipelines to be an explicit nil

### UnsetPipelines
`func (o *PipelineService) UnsetPipelines()`

UnsetPipelines ensures that no value is present for Pipelines, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


