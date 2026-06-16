# TerminalService

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**ShellTool** | Pointer to **map[string]interface{}** |  | [optional] 
**Bus** | Pointer to **map[string]interface{}** |  | [optional] 
**Logger** | Pointer to **map[string]interface{}** |  | [optional] 
**History** | Pointer to **[]string** |  | [optional] 
**HistoryMu** | Pointer to **map[string]interface{}** |  | [optional] 
**MaxHistory** | Pointer to **int32** |  | [optional] 
**WorkingDir** | Pointer to **string** |  | [optional] 
**SessionStore** | Pointer to **NullableString** |  | [optional] 
**SessionMu** | Pointer to **map[string]interface{}** |  | [optional] 

## Methods

### NewTerminalService

`func NewTerminalService() *TerminalService`

NewTerminalService instantiates a new TerminalService object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewTerminalServiceWithDefaults

`func NewTerminalServiceWithDefaults() *TerminalService`

NewTerminalServiceWithDefaults instantiates a new TerminalService object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetShellTool

`func (o *TerminalService) GetShellTool() map[string]interface{}`

GetShellTool returns the ShellTool field if non-nil, zero value otherwise.

### GetShellToolOk

`func (o *TerminalService) GetShellToolOk() (*map[string]interface{}, bool)`

GetShellToolOk returns a tuple with the ShellTool field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetShellTool

`func (o *TerminalService) SetShellTool(v map[string]interface{})`

SetShellTool sets ShellTool field to given value.

### HasShellTool

`func (o *TerminalService) HasShellTool() bool`

HasShellTool returns a boolean if a field has been set.

### SetShellToolNil

`func (o *TerminalService) SetShellToolNil(b bool)`

 SetShellToolNil sets the value for ShellTool to be an explicit nil

### UnsetShellTool
`func (o *TerminalService) UnsetShellTool()`

UnsetShellTool ensures that no value is present for ShellTool, not even an explicit nil
### GetBus

`func (o *TerminalService) GetBus() map[string]interface{}`

GetBus returns the Bus field if non-nil, zero value otherwise.

### GetBusOk

`func (o *TerminalService) GetBusOk() (*map[string]interface{}, bool)`

GetBusOk returns a tuple with the Bus field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetBus

`func (o *TerminalService) SetBus(v map[string]interface{})`

SetBus sets Bus field to given value.

### HasBus

`func (o *TerminalService) HasBus() bool`

HasBus returns a boolean if a field has been set.

### SetBusNil

`func (o *TerminalService) SetBusNil(b bool)`

 SetBusNil sets the value for Bus to be an explicit nil

### UnsetBus
`func (o *TerminalService) UnsetBus()`

UnsetBus ensures that no value is present for Bus, not even an explicit nil
### GetLogger

`func (o *TerminalService) GetLogger() map[string]interface{}`

GetLogger returns the Logger field if non-nil, zero value otherwise.

### GetLoggerOk

`func (o *TerminalService) GetLoggerOk() (*map[string]interface{}, bool)`

GetLoggerOk returns a tuple with the Logger field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLogger

`func (o *TerminalService) SetLogger(v map[string]interface{})`

SetLogger sets Logger field to given value.

### HasLogger

`func (o *TerminalService) HasLogger() bool`

HasLogger returns a boolean if a field has been set.

### SetLoggerNil

`func (o *TerminalService) SetLoggerNil(b bool)`

 SetLoggerNil sets the value for Logger to be an explicit nil

### UnsetLogger
`func (o *TerminalService) UnsetLogger()`

UnsetLogger ensures that no value is present for Logger, not even an explicit nil
### GetHistory

`func (o *TerminalService) GetHistory() []string`

GetHistory returns the History field if non-nil, zero value otherwise.

### GetHistoryOk

`func (o *TerminalService) GetHistoryOk() (*[]string, bool)`

GetHistoryOk returns a tuple with the History field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetHistory

`func (o *TerminalService) SetHistory(v []string)`

SetHistory sets History field to given value.

### HasHistory

`func (o *TerminalService) HasHistory() bool`

HasHistory returns a boolean if a field has been set.

### SetHistoryNil

`func (o *TerminalService) SetHistoryNil(b bool)`

 SetHistoryNil sets the value for History to be an explicit nil

### UnsetHistory
`func (o *TerminalService) UnsetHistory()`

UnsetHistory ensures that no value is present for History, not even an explicit nil
### GetHistoryMu

`func (o *TerminalService) GetHistoryMu() map[string]interface{}`

GetHistoryMu returns the HistoryMu field if non-nil, zero value otherwise.

### GetHistoryMuOk

`func (o *TerminalService) GetHistoryMuOk() (*map[string]interface{}, bool)`

GetHistoryMuOk returns a tuple with the HistoryMu field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetHistoryMu

`func (o *TerminalService) SetHistoryMu(v map[string]interface{})`

SetHistoryMu sets HistoryMu field to given value.

### HasHistoryMu

`func (o *TerminalService) HasHistoryMu() bool`

HasHistoryMu returns a boolean if a field has been set.

### GetMaxHistory

`func (o *TerminalService) GetMaxHistory() int32`

GetMaxHistory returns the MaxHistory field if non-nil, zero value otherwise.

### GetMaxHistoryOk

`func (o *TerminalService) GetMaxHistoryOk() (*int32, bool)`

GetMaxHistoryOk returns a tuple with the MaxHistory field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetMaxHistory

`func (o *TerminalService) SetMaxHistory(v int32)`

SetMaxHistory sets MaxHistory field to given value.

### HasMaxHistory

`func (o *TerminalService) HasMaxHistory() bool`

HasMaxHistory returns a boolean if a field has been set.

### GetWorkingDir

`func (o *TerminalService) GetWorkingDir() string`

GetWorkingDir returns the WorkingDir field if non-nil, zero value otherwise.

### GetWorkingDirOk

`func (o *TerminalService) GetWorkingDirOk() (*string, bool)`

GetWorkingDirOk returns a tuple with the WorkingDir field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetWorkingDir

`func (o *TerminalService) SetWorkingDir(v string)`

SetWorkingDir sets WorkingDir field to given value.

### HasWorkingDir

`func (o *TerminalService) HasWorkingDir() bool`

HasWorkingDir returns a boolean if a field has been set.

### GetSessionStore

`func (o *TerminalService) GetSessionStore() string`

GetSessionStore returns the SessionStore field if non-nil, zero value otherwise.

### GetSessionStoreOk

`func (o *TerminalService) GetSessionStoreOk() (*string, bool)`

GetSessionStoreOk returns a tuple with the SessionStore field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSessionStore

`func (o *TerminalService) SetSessionStore(v string)`

SetSessionStore sets SessionStore field to given value.

### HasSessionStore

`func (o *TerminalService) HasSessionStore() bool`

HasSessionStore returns a boolean if a field has been set.

### SetSessionStoreNil

`func (o *TerminalService) SetSessionStoreNil(b bool)`

 SetSessionStoreNil sets the value for SessionStore to be an explicit nil

### UnsetSessionStore
`func (o *TerminalService) UnsetSessionStore()`

UnsetSessionStore ensures that no value is present for SessionStore, not even an explicit nil
### GetSessionMu

`func (o *TerminalService) GetSessionMu() map[string]interface{}`

GetSessionMu returns the SessionMu field if non-nil, zero value otherwise.

### GetSessionMuOk

`func (o *TerminalService) GetSessionMuOk() (*map[string]interface{}, bool)`

GetSessionMuOk returns a tuple with the SessionMu field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSessionMu

`func (o *TerminalService) SetSessionMu(v map[string]interface{})`

SetSessionMu sets SessionMu field to given value.

### HasSessionMu

`func (o *TerminalService) HasSessionMu() bool`

HasSessionMu returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


