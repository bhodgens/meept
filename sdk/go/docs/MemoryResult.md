# MemoryResult

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Memory** | **map[string]interface{}** |  | 
**RelevanceScore** | **float32** |  | 
**Source** | **string** |  | 

## Methods

### NewMemoryResult

`func NewMemoryResult(memory map[string]interface{}, relevanceScore float32, source string, ) *MemoryResult`

NewMemoryResult instantiates a new MemoryResult object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewMemoryResultWithDefaults

`func NewMemoryResultWithDefaults() *MemoryResult`

NewMemoryResultWithDefaults instantiates a new MemoryResult object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetMemory

`func (o *MemoryResult) GetMemory() map[string]interface{}`

GetMemory returns the Memory field if non-nil, zero value otherwise.

### GetMemoryOk

`func (o *MemoryResult) GetMemoryOk() (*map[string]interface{}, bool)`

GetMemoryOk returns a tuple with the Memory field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetMemory

`func (o *MemoryResult) SetMemory(v map[string]interface{})`

SetMemory sets Memory field to given value.


### GetRelevanceScore

`func (o *MemoryResult) GetRelevanceScore() float32`

GetRelevanceScore returns the RelevanceScore field if non-nil, zero value otherwise.

### GetRelevanceScoreOk

`func (o *MemoryResult) GetRelevanceScoreOk() (*float32, bool)`

GetRelevanceScoreOk returns a tuple with the RelevanceScore field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetRelevanceScore

`func (o *MemoryResult) SetRelevanceScore(v float32)`

SetRelevanceScore sets RelevanceScore field to given value.


### GetSource

`func (o *MemoryResult) GetSource() string`

GetSource returns the Source field if non-nil, zero value otherwise.

### GetSourceOk

`func (o *MemoryResult) GetSourceOk() (*string, bool)`

GetSourceOk returns a tuple with the Source field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSource

`func (o *MemoryResult) SetSource(v string)`

SetSource sets Source field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


