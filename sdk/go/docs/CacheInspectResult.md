# CacheInspectResult

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**PromptHash** | **string** |  | 
**ModelId** | **string** |  | 
**CreatedAt** | **string** |  | 
**ExpiresAt** | **string** |  | 
**HitCount** | **int32** |  | 
**FileHashesomitempty** | Pointer to **NullableString** |  | [optional] 
**Source** | **string** |  | 

## Methods

### NewCacheInspectResult

`func NewCacheInspectResult(promptHash string, modelId string, createdAt string, expiresAt string, hitCount int32, source string, ) *CacheInspectResult`

NewCacheInspectResult instantiates a new CacheInspectResult object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewCacheInspectResultWithDefaults

`func NewCacheInspectResultWithDefaults() *CacheInspectResult`

NewCacheInspectResultWithDefaults instantiates a new CacheInspectResult object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetPromptHash

`func (o *CacheInspectResult) GetPromptHash() string`

GetPromptHash returns the PromptHash field if non-nil, zero value otherwise.

### GetPromptHashOk

`func (o *CacheInspectResult) GetPromptHashOk() (*string, bool)`

GetPromptHashOk returns a tuple with the PromptHash field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPromptHash

`func (o *CacheInspectResult) SetPromptHash(v string)`

SetPromptHash sets PromptHash field to given value.


### GetModelId

`func (o *CacheInspectResult) GetModelId() string`

GetModelId returns the ModelId field if non-nil, zero value otherwise.

### GetModelIdOk

`func (o *CacheInspectResult) GetModelIdOk() (*string, bool)`

GetModelIdOk returns a tuple with the ModelId field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetModelId

`func (o *CacheInspectResult) SetModelId(v string)`

SetModelId sets ModelId field to given value.


### GetCreatedAt

`func (o *CacheInspectResult) GetCreatedAt() string`

GetCreatedAt returns the CreatedAt field if non-nil, zero value otherwise.

### GetCreatedAtOk

`func (o *CacheInspectResult) GetCreatedAtOk() (*string, bool)`

GetCreatedAtOk returns a tuple with the CreatedAt field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetCreatedAt

`func (o *CacheInspectResult) SetCreatedAt(v string)`

SetCreatedAt sets CreatedAt field to given value.


### GetExpiresAt

`func (o *CacheInspectResult) GetExpiresAt() string`

GetExpiresAt returns the ExpiresAt field if non-nil, zero value otherwise.

### GetExpiresAtOk

`func (o *CacheInspectResult) GetExpiresAtOk() (*string, bool)`

GetExpiresAtOk returns a tuple with the ExpiresAt field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetExpiresAt

`func (o *CacheInspectResult) SetExpiresAt(v string)`

SetExpiresAt sets ExpiresAt field to given value.


### GetHitCount

`func (o *CacheInspectResult) GetHitCount() int32`

GetHitCount returns the HitCount field if non-nil, zero value otherwise.

### GetHitCountOk

`func (o *CacheInspectResult) GetHitCountOk() (*int32, bool)`

GetHitCountOk returns a tuple with the HitCount field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetHitCount

`func (o *CacheInspectResult) SetHitCount(v int32)`

SetHitCount sets HitCount field to given value.


### GetFileHashesomitempty

`func (o *CacheInspectResult) GetFileHashesomitempty() string`

GetFileHashesomitempty returns the FileHashesomitempty field if non-nil, zero value otherwise.

### GetFileHashesomitemptyOk

`func (o *CacheInspectResult) GetFileHashesomitemptyOk() (*string, bool)`

GetFileHashesomitemptyOk returns a tuple with the FileHashesomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetFileHashesomitempty

`func (o *CacheInspectResult) SetFileHashesomitempty(v string)`

SetFileHashesomitempty sets FileHashesomitempty field to given value.

### HasFileHashesomitempty

`func (o *CacheInspectResult) HasFileHashesomitempty() bool`

HasFileHashesomitempty returns a boolean if a field has been set.

### SetFileHashesomitemptyNil

`func (o *CacheInspectResult) SetFileHashesomitemptyNil(b bool)`

 SetFileHashesomitemptyNil sets the value for FileHashesomitempty to be an explicit nil

### UnsetFileHashesomitempty
`func (o *CacheInspectResult) UnsetFileHashesomitempty()`

UnsetFileHashesomitempty ensures that no value is present for FileHashesomitempty, not even an explicit nil
### GetSource

`func (o *CacheInspectResult) GetSource() string`

GetSource returns the Source field if non-nil, zero value otherwise.

### GetSourceOk

`func (o *CacheInspectResult) GetSourceOk() (*string, bool)`

GetSourceOk returns a tuple with the Source field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSource

`func (o *CacheInspectResult) SetSource(v string)`

SetSource sets Source field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


