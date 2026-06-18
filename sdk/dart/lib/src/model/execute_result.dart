//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'execute_result.g.dart';

/// ExecuteResult
///
/// Properties:
/// * [output] 
/// * [success] 
/// * [errorCommaOmitempty] 
@BuiltValue()
abstract class ExecuteResult implements Built<ExecuteResult, ExecuteResultBuilder> {
  @BuiltValueField(wireName: r'output')
  String get output;

  @BuiltValueField(wireName: r'success')
  bool get success;

  @BuiltValueField(wireName: r'error,omitempty')
  String? get errorCommaOmitempty;

  ExecuteResult._();

  factory ExecuteResult([void updates(ExecuteResultBuilder b)]) = _$ExecuteResult;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ExecuteResultBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ExecuteResult> get serializer => _$ExecuteResultSerializer();
}

class _$ExecuteResultSerializer implements PrimitiveSerializer<ExecuteResult> {
  @override
  final Iterable<Type> types = const [ExecuteResult, _$ExecuteResult];

  @override
  final String wireName = r'ExecuteResult';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ExecuteResult object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'output';
    yield serializers.serialize(
      object.output,
      specifiedType: const FullType(String),
    );
    yield r'success';
    yield serializers.serialize(
      object.success,
      specifiedType: const FullType(bool),
    );
    if (object.errorCommaOmitempty != null) {
      yield r'error,omitempty';
      yield serializers.serialize(
        object.errorCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    ExecuteResult object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ExecuteResultBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'output':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.output = valueDes;
          break;
        case r'success':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.success = valueDes;
          break;
        case r'error,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.errorCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ExecuteResult deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ExecuteResultBuilder();
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

