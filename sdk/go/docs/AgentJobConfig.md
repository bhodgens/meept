# AgentJobConfig

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Prompt** | **string** |  | 
**Contextomitempty** | Pointer to **NullableString** |  | [optional] 
**Modelomitempty** | Pointer to **string** |  | [optional] 
**MaxTokensomitempty** | Pointer to **int32** |  | [optional] 
**Temperatureomitempty** | Pointer to **float32** |  | [optional] 

## Methods

### NewAgentJobConfig

`func NewAgentJobConfig(prompt string, ) *AgentJobConfig`

NewAgentJobConfig instantiates a new AgentJobConfig object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewAgentJobConfigWithDefaults

`func NewAgentJobConfigWithDefaults() *AgentJobConfig`

NewAgentJobConfigWithDefaults instantiates a new AgentJobConfig object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetPrompt

`func (o *AgentJobConfig) GetPrompt() string`

GetPrompt returns the Prompt field if non-nil, zero value otherwise.

### GetPromptOk

`func (o *AgentJobConfig) GetPromptOk() (*string, bool)`

GetPromptOk returns a tuple with the Prompt field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPrompt

`func (o *AgentJobConfig) SetPrompt(v string)`

SetPrompt sets Prompt field to given value.


### GetContextomitempty

`func (o *AgentJobConfig) GetContextomitempty() string`

GetContextomitempty returns the Contextomitempty field if non-nil, zero value otherwise.

### GetContextomitemptyOk

`func (o *AgentJobConfig) GetContextomitemptyOk() (*string, bool)`

GetContextomitemptyOk returns a tuple with the Contextomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetContextomitempty

`func (o *AgentJobConfig) SetContextomitempty(v string)`

SetContextomitempty sets Contextomitempty field to given value.

### HasContextomitempty

`func (o *AgentJobConfig) HasContextomitempty() bool`

HasContextomitempty returns a boolean if a field has been set.

### SetContextomitemptyNil

`func (o *AgentJobConfig) SetContextomitemptyNil(b bool)`

 SetContextomitemptyNil sets the value for Contextomitempty to be an explicit nil

### UnsetContextomitempty
`func (o *AgentJobConfig) UnsetContextomitempty()`

UnsetContextomitempty ensures that no value is present for Contextomitempty, not even an explicit nil
### GetModelomitempty

`func (o *AgentJobConfig) GetModelomitempty() string`

GetModelomitempty returns the Modelomitempty field if non-nil, zero value otherwise.

### GetModelomitemptyOk

`func (o *AgentJobConfig) GetModelomitemptyOk() (*string, bool)`

GetModelomitemptyOk returns a tuple with the Modelomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetModelomitempty

`func (o *AgentJobConfig) SetModelomitempty(v string)`

SetModelomitempty sets Modelomitempty field to given value.

### HasModelomitempty

`func (o *AgentJobConfig) HasModelomitempty() bool`

HasModelomitempty returns a boolean if a field has been set.

### GetMaxTokensomitempty

`func (o *AgentJobConfig) GetMaxTokensomitempty() int32`

GetMaxTokensomitempty returns the MaxTokensomitempty field if non-nil, zero value otherwise.

### GetMaxTokensomitemptyOk

`func (o *AgentJobConfig) GetMaxTokensomitemptyOk() (*int32, bool)`

GetMaxTokensomitemptyOk returns a tuple with the MaxTokensomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetMaxTokensomitempty

`func (o *AgentJobConfig) SetMaxTokensomitempty(v int32)`

SetMaxTokensomitempty sets MaxTokensomitempty field to given value.

### HasMaxTokensomitempty

`func (o *AgentJobConfig) HasMaxTokensomitempty() bool`

HasMaxTokensomitempty returns a boolean if a field has been set.

### GetTemperatureomitempty

`func (o *AgentJobConfig) GetTemperatureomitempty() float32`

GetTemperatureomitempty returns the Temperatureomitempty field if non-nil, zero value otherwise.

### GetTemperatureomitemptyOk

`func (o *AgentJobConfig) GetTemperatureomitemptyOk() (*float32, bool)`

GetTemperatureomitemptyOk returns a tuple with the Temperatureomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTemperatureomitempty

`func (o *AgentJobConfig) SetTemperatureomitempty(v float32)`

SetTemperatureomitempty sets Temperatureomitempty field to given value.

### HasTemperatureomitempty

`func (o *AgentJobConfig) HasTemperatureomitempty() bool`

HasTemperatureomitempty returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


