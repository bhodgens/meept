//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'pipeline_list_request.g.dart';

/// PipelineListRequest
///
/// Properties:
/// * [limitCommaOmitempty] 
@BuiltValue()
abstract class PipelineListRequest implements Built<PipelineListRequest, PipelineListRequestBuilder> {
  @BuiltValueField(wireName: r'limit,omitempty')
  int? get limitCommaOmitempty;

  PipelineListRequest._();

  factory PipelineListRequest([void updates(PipelineListRequestBuilder b)]) = _$PipelineListRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(PipelineListRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<PipelineListRequest> get serializer => _$PipelineListRequestSerializer();
}

class _$PipelineListRequestSerializer implements PrimitiveSerializer<PipelineListRequest> {
  @override
  final Iterable<Type> types = const [PipelineListRequest, _$PipelineListRequest];

  @override
  final String wireName = r'PipelineListRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    PipelineListRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.limitCommaOmitempty != null) {
      yield r'limit,omitempty';
      yield serializers.serialize(
        object.limitCommaOmitempty,
        specifiedType: const FullType(int),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    PipelineListRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required PipelineListRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'limit,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.limitCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  PipelineListRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = PipelineListRequestBuilder();
    final serializedList = (serialized as Iterable<Object?>).toList();
    final unhandled = <Object?>[];
    _deserializeProperties(
      serializers,
      serialized,
      specifiedType: specifiedType,
      serializedList: serializedList,
      unhandled: unhandled,
      result: result,
    );
    return result.build();
  }
}

