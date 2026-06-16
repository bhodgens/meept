# VectorSearchResult

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**MemoryId** | **string** |  | 
**Content** | **string** |  | 
**Metadataomitempty** | Pointer to **NullableString** |  | [optional] 
**RelevanceScore** | **float32** |  | 
**VectorSimilarity** | **float32** |  | 

## Methods

### NewVectorSearchResult

`func NewVectorSearchResult(memoryId string, content string, relevanceScore float32, vectorSimilarity float32, ) *VectorSearchResult`

NewVectorSearchResult instantiates a new VectorSearchResult object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewVectorSearchResultWithDefaults

`func NewVectorSearchResultWithDefaults() *VectorSearchResult`

NewVectorSearchResultWithDefaults instantiates a new VectorSearchResult object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetMemoryId

`func (o *VectorSearchResult) GetMemoryId() string`

GetMemoryId returns the MemoryId field if non-nil, zero value otherwise.

### GetMemoryIdOk

`func (o *VectorSearchResult) GetMemoryIdOk() (*string, bool)`

GetMemoryIdOk returns a tuple with the MemoryId field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetMemoryId

`func (o *VectorSearchResult) SetMemoryId(v string)`

SetMemoryId sets MemoryId field to given value.


### GetContent

`func (o *VectorSearchResult) GetContent() string`

GetContent returns the Content field if non-nil, zero value otherwise.

### GetContentOk

`func (o *VectorSearchResult) GetContentOk() (*string, bool)`

GetContentOk returns a tuple with the Content field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetContent

`func (o *VectorSearchResult) SetContent(v string)`

SetContent sets Content field to given value.


### GetMetadataomitempty

`func (o *VectorSearchResult) GetMetadataomitempty() string`

GetMetadataomitempty returns the Metadataomitempty field if non-nil, zero value otherwise.

### GetMetadataomitemptyOk

`func (o *VectorSearchResult) GetMetadataomitemptyOk() (*string, bool)`

GetMetadataomitemptyOk returns a tuple with the Metadataomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetMetadataomitempty

`func (o *VectorSearchResult) SetMetadataomitempty(v string)`

SetMetadataomitempty sets Metadataomitempty field to given value.

### HasMetadataomitempty

`func (o *VectorSearchResult) HasMetadataomitempty() bool`

HasMetadataomitempty returns a boolean if a field has been set.

### SetMetadataomitemptyNil

`func (o *VectorSearchResult) SetMetadataomitemptyNil(b bool)`

 SetMetadataomitemptyNil sets the value for Metadataomitempty to be an explicit nil

### UnsetMetadataomitempty
`func (o *VectorSearchResult) UnsetMetadataomitempty()`

UnsetMetadataomitempty ensures that no value is present for Metadataomitempty, not even an explicit nil
### GetRelevanceScore

`func (o *VectorSearchResult) GetRelevanceScore() float32`

GetRelevanceScore returns the RelevanceScore field if non-nil, zero value otherwise.

### GetRelevanceScoreOk

`func (o *VectorSearchResult) GetRelevanceScoreOk() (*float32, bool)`

GetRelevanceScoreOk returns a tuple with the RelevanceScore field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetRelevanceScore

`func (o *VectorSearchResult) SetRelevanceScore(v float32)`

SetRelevanceScore sets RelevanceScore field to given value.


### GetVectorSimilarity

`func (o *VectorSearchResult) GetVectorSimilarity() float32`

GetVectorSimilarity returns the VectorSimilarity field if non-nil, zero value otherwise.

### GetVectorSimilarityOk

`func (o *VectorSearchResult) GetVectorSimilarityOk() (*float32, bool)`

GetVectorSimilarityOk returns a tuple with the VectorSimilarity field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetVectorSimilarity

`func (o *VectorSearchResult) SetVectorSimilarity(v float32)`

SetVectorSimilarity sets VectorSimilarity field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


