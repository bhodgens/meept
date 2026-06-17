// ignore_for_file: public_member_api_docs

/// Custom lint rule: `enum_name_shadowing`.
///
/// Flags any extension getter named `name`, `index`, `hashCode`, or
/// `runtimeType` declared on a Dart `enum` type. In Dart 2.15+, every
/// `enum` declaration synthesizes `String get name` and `int get index`
/// getters that silently shadow any extension getter with the same
/// name. The Dart analyzer does NOT warn about this — it is a
/// language-level footgun.
///
/// Origin: Round 6 of the meept codebase review found that
/// `SearchScopeX` declared `String get name`, which was silently
/// shadowed by `Enum.name` at every call site. Search requests were
/// sent with the wrong scope identifier. The fix was to rename the
/// getter to `apiValue`.
///
/// ## Activation
///
/// This rule lives in a standalone package so it can be added without
/// disturbing the app's analyzer/build_runner versions. To enable:
///
/// 1. Add this directory as a path dev-dependency in the app's
///    `pubspec.yaml`:
///    ```yaml
///    dev_dependencies:
///      meept_lints:
///        path: tools/lints
///    ```
/// 2. Ensure `analysis_options.yaml` lists `custom_lint` under
///    `analyzer.plugins` (already configured).
/// 3. Run `flutter pub get` then `dart run custom_lint`.
///
/// IMPORTANT: Adding `custom_lint` requires `analyzer_plugin` and
/// transitively bumps `analyzer` to 7.x and `build_runner` to 2.5+.
/// Do this in a dedicated commit so the build-output diff can be
/// reviewed separately.
///
/// ## Why not just a dart analyze rule?
///
/// The built-in analyzer rules do not cover enum-extension shadowing
/// (as of Dart 3.5). `prefer_typing_uninitialized_variables` and
/// friends don't catch this pattern. See
/// https://github.com/dart-lang/sdk/issues/54937 for upstream status.
library;

import 'package:analyzer/dart/ast/ast.dart';
import 'package:analyzer/dart/ast/visitor.dart';
import 'package:analyzer/dart/element/element.dart';
import 'package:analyzer/dart/element/type.dart';
import 'package:custom_lint_builder/custom_lint_builder.dart';

/// Names Dart synthesizes on enums or inherits from Object.
const _shadowedNames = <String, String>{
  'name': 'Enum.name',
  'index': 'Enum.index',
  'hashCode': 'Object.hashCode',
  'runtimeType': 'Object.runtimeType',
};

PluginBase createPlugin() => _MeeptLintPlugin();

class _MeeptLintPlugin extends PluginBase {
  @override
  List<LintRule> getLintRules(CustomLintConfigs configs) {
    return [
      const _EnumNameShadowingRule(),
    ];
  }
}

class _EnumNameShadowingRule extends DartLintRule {
  const _EnumNameShadowingRule();

  @override
  Metadata get metadata => const Metadata(
        name: 'enum_name_shadowing',
        message: 'Extension getter shadows a synthesized Enum property',
        description: _description,
      );

  static const _description = '''
In Dart 2.15+, every `enum` declaration synthesizes a `String get name`
and `int get index` getter. If an extension method declares a getter
with the same name on an enum type, the synthesized property wins at
every call site and the compiler does NOT warn. This is a
language-level footgun.

DO rename the getter (e.g. `apiValue`, `displayName`, `enumIndex`).
DO NOT name extension getters on enums `name`, `index`, `hashCode`,
or `runtimeType`.
''';

  @override
  void run(
    CustomLintResolver resolver,
    ErrorReporter reporter,
    CustomLintContext context,
  ) {
    context.registry.addExtensionDeclaration((node) {
      final onType = node.extendedType.type;
      if (onType is! InterfaceType) return;
      if (onType.element is! EnumElement) return;

      for (final member in node.members) {
        if (member is MethodDeclaration && member.isGetter) {
          final nameToken = member.name;
          final getterName = nameToken.lexeme;
          final shadowedBy = _shadowedNames[getterName];
          if (shadowedBy == null) continue;
          reporter.atToken(
            nameToken,
            _EnumNameShadowingCode.forGetter(getterName, shadowedBy),
          );
        }
      }
    });
  }
}

/// Lint code object — one per shadowed-name variant so messages are
/// precise at the call site.
class _EnumNameShadowingCode extends LintCode {
  _EnumNameShadowingCode._({
    required super.name,
    required super.problemMessage,
    required super.correctionMessage,
  });

  static LintCode forGetter(String getterName, String shadowedBy) {
    return _EnumNameShadowingCode._(
      name: 'enum_name_shadowing',
      problemMessage:
          "Extension getter '$getterName' is shadowed by Dart's "
          "synthesized $shadowedBy and will never be invoked.",
      correctionMessage: 'Rename the getter (e.g. `apiValue`, '
          '`displayName`) so it does not collide with the synthesized '
          'enum property.',
    );
  }
}
