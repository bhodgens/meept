//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

library openapi.api;

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:collection/collection.dart';
import 'package:http/http.dart';
import 'package:intl/intl.dart';
import 'package:meta/meta.dart';

part 'api_client.dart';
part 'api_helper.dart';
part 'api_exception.dart';
part 'auth/authentication.dart';
part 'auth/api_key_auth.dart';
part 'auth/oauth.dart';
part 'auth/http_basic_auth.dart';
part 'auth/http_bearer_auth.dart';

part 'api/health_api.dart';
part 'api/v1_api.dart';
part 'api/web_socket_api.dart';

part 'model/add_job_request.dart';
part 'model/add_job_response.dart';
part 'model/add_worker_request.dart';
part 'model/agent_job_config.dart';
part 'model/agent_progress_event.dart';
part 'model/apply_improvement_request.dart';
part 'model/approve_plan_request.dart';
part 'model/attach_session_request.dart';
part 'model/attendee_info.dart';
part 'model/audit_entry.dart';
part 'model/audit_request.dart';
part 'model/branch_session_request.dart';
part 'model/bus_service.dart';
part 'model/bus_stats_response.dart';
part 'model/cache_inspect_result.dart';
part 'model/cache_service.dart';
part 'model/cache_stats_response.dart';
part 'model/calendar_event.dart';
part 'model/calendar_service.dart';
part 'model/cancel_request.dart';
part 'model/cancel_task_request.dart';
part 'model/chat_request.dart';
part 'model/chat_response.dart';
part 'model/chat_service.dart';
part 'model/check_request.dart';
part 'model/check_response.dart';
part 'model/claim_request.dart';
part 'model/clear_cache_request.dart';
part 'model/command_history.dart';
part 'model/compact_session_request.dart';
part 'model/complete_request.dart';
part 'model/config.dart';
part 'model/confirm_plan_request.dart';
part 'model/create_event_request.dart';
part 'model/create_pipeline_request.dart';
part 'model/create_plan_request.dart';
part 'model/create_session_request.dart';
part 'model/create_task_request.dart';
part 'model/daemon_service.dart';
part 'model/daemon_status.dart';
part 'model/delete_pipeline_request.dart';
part 'model/delete_session_request.dart';
part 'model/delete_task_request.dart';
part 'model/detach_session_request.dart';
part 'model/enable_job_request.dart';
part 'model/enqueue_request.dart';
part 'model/execute_request.dart';
part 'model/execute_result.dart';
part 'model/fail_request.dart';
part 'model/follow_up_request.dart';
part 'model/fork_session_request.dart';
part 'model/generate_improvement_request.dart';
part 'model/get_messages_request.dart';
part 'model/get_request.dart';
part 'model/get_session_request.dart';
part 'model/get_task_request.dart';
part 'model/get_task_steps_request.dart';
part 'model/get_tree_request.dart';
part 'model/invalidate_request.dart';
part 'model/list_branches_request.dart';
part 'model/list_events_request.dart';
part 'model/list_events_response.dart';
part 'model/list_jobs_response.dart';
part 'model/list_options.dart';
part 'model/list_request.dart';
part 'model/list_sessions_request.dart';
part 'model/memory_query_request.dart';
part 'model/memory_result.dart';
part 'model/memory_service.dart';
part 'model/model_info.dart';
part 'model/model_service.dart';
part 'model/paginated_response.dart';
part 'model/pause_job_request.dart';
part 'model/pipeline.dart';
part 'model/pipeline_info.dart';
part 'model/pipeline_list_request.dart';
part 'model/pipeline_service.dart';
part 'model/pipeline_status_response.dart';
part 'model/pipeline_step.dart';
part 'model/pipeline_step_status.dart';
part 'model/plan_service.dart';
part 'model/project_service.dart';
part 'model/provider_info.dart';
part 'model/publish_request.dart';
part 'model/queue_service.dart';
part 'model/queue_status_request.dart';
part 'model/queue_status_response.dart';
part 'model/register_project_request.dart';
part 'model/reject_improvement_request.dart';
part 'model/reject_plan_request.dart';
part 'model/remove_job_request.dart';
part 'model/remove_worker_request.dart';
part 'model/resume_job_request.dart';
part 'model/resume_session_request.dart';
part 'model/retry_request.dart';
part 'model/revise_plan_request.dart';
part 'model/runtime_service.dart';
part 'model/runtime_status_response.dart';
part 'model/scale_workers_request.dart';
part 'model/scheduler_service.dart';
part 'model/search_request.dart';
part 'model/search_result.dart';
part 'model/search_service.dart';
part 'model/security_service.dart';
part 'model/self_improve_service.dart';
part 'model/service_error.dart';
part 'model/service_registry.dart';
part 'model/session_service.dart';
part 'model/set_project_request.dart';
part 'model/shard_detail.dart';
part 'model/shell_job_config.dart';
part 'model/skill_info.dart';
part 'model/skill_ui_descriptor.dart';
part 'model/skills_get_request.dart';
part 'model/skills_list_request.dart';
part 'model/skills_service.dart';
part 'model/status_request.dart';
part 'model/status_response.dart';
part 'model/steer_request.dart';
part 'model/task_list_request.dart';
part 'model/task_service.dart';
part 'model/template_info.dart';
part 'model/templates_clear_request.dart';
part 'model/templates_clear_result.dart';
part 'model/templates_get_request.dart';
part 'model/templates_invoke_request.dart';
part 'model/templates_invoke_result.dart';
part 'model/templates_list_request.dart';
part 'model/templates_service.dart';
part 'model/terminal_service.dart';
part 'model/terminal_session.dart';
part 'model/trigger_request.dart';
part 'model/ui_action_def.dart';
part 'model/ui_field_def.dart';
part 'model/update_event_request.dart';
part 'model/update_status_request.dart';
part 'model/update_task_request.dart';
part 'model/validate_improvement_request.dart';
part 'model/vector_search_request.dart';
part 'model/vector_search_result.dart';
part 'model/vector_stats.dart';
part 'model/vector_store_request.dart';
part 'model/ws_subscribe_message.dart';
part 'model/ws_unsubscribe_message.dart';
part 'model/worker_service.dart';
part 'model/worker_stats_response.dart';


/// An [ApiClient] instance that uses the default values obtained from
/// the OpenAPI specification file.
var defaultApiClient = ApiClient();

const _delimiters = {'csv': ',', 'ssv': ' ', 'tsv': '\t', 'pipes': '|'};
const _dateEpochMarker = 'epoch';
const _deepEquality = DeepCollectionEquality();
final _dateFormatter = DateFormat('yyyy-MM-dd');
final _regList = RegExp(r'^List<(.*)>$');
final _regSet = RegExp(r'^Set<(.*)>$');
final _regMap = RegExp(r'^Map<String,(.*)>$');

bool _isEpochMarker(String? pattern) => pattern == _dateEpochMarker || pattern == '/$_dateEpochMarker/';
