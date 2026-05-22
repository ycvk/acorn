import 'package:flutter/material.dart';
import 'package:flex_color_scheme/flex_color_scheme.dart';

ThemeData buildAcornTheme(Brightness brightness) {
  final scheme = _acornColorScheme(brightness);
  final base = brightness == Brightness.light
      ? FlexThemeData.light(
          colorScheme: scheme,
          surfaceMode: FlexSurfaceMode.highScaffoldLowSurface,
          blendLevel: 4,
          subThemesData: _acornSubThemes(Brightness.light),
          useMaterial3: true,
          visualDensity: FlexColorScheme.comfortablePlatformDensity,
        )
      : FlexThemeData.dark(
          colorScheme: scheme,
          surfaceMode: FlexSurfaceMode.highScaffoldLowSurface,
          blendLevel: 8,
          subThemesData: _acornSubThemes(Brightness.dark),
          useMaterial3: true,
          visualDensity: FlexColorScheme.comfortablePlatformDensity,
        );
  final baseScheme = base.colorScheme;
  final acornScheme = baseScheme.copyWith(
    primary: scheme.primary,
    onPrimary: scheme.onPrimary,
    primaryContainer: scheme.primaryContainer,
    onPrimaryContainer: scheme.onPrimaryContainer,
    secondary: scheme.secondary,
    onSecondary: scheme.onSecondary,
    secondaryContainer: scheme.secondaryContainer,
    onSecondaryContainer: scheme.onSecondaryContainer,
    tertiary: scheme.tertiary,
    onTertiary: scheme.onTertiary,
    tertiaryContainer: scheme.tertiaryContainer,
    onTertiaryContainer: scheme.onTertiaryContainer,
    error: scheme.error,
    onError: scheme.onError,
    errorContainer: scheme.errorContainer,
    onErrorContainer: scheme.onErrorContainer,
    surface: scheme.surface,
    onSurface: scheme.onSurface,
    surfaceContainerLowest: scheme.surfaceContainerLowest,
    surfaceContainerLow: scheme.surfaceContainerLow,
    surfaceContainer: scheme.surfaceContainer,
    surfaceContainerHigh: scheme.surfaceContainerHigh,
    surfaceContainerHighest: scheme.surfaceContainerHighest,
    onSurfaceVariant: scheme.onSurfaceVariant,
    outline: scheme.outline,
    outlineVariant: scheme.outlineVariant,
    inverseSurface: scheme.inverseSurface,
    onInverseSurface: scheme.onInverseSurface,
    inversePrimary: scheme.inversePrimary,
    surfaceTint: scheme.surfaceTint,
  );
  final seeded = base.copyWith(
    brightness: brightness,
    colorScheme: acornScheme,
    scaffoldBackgroundColor: acornScheme.surface,
  );
  final textTheme = seeded.textTheme.apply(
    bodyColor: acornScheme.onSurface,
    displayColor: acornScheme.onSurface,
  );

  return seeded.copyWith(
    scaffoldBackgroundColor: acornScheme.surface,
    textTheme: textTheme.copyWith(
      headlineMedium: textTheme.headlineMedium?.copyWith(
        fontWeight: FontWeight.w800,
        height: 1.08,
      ),
      titleLarge: textTheme.titleLarge?.copyWith(
        fontWeight: FontWeight.w700,
        height: 1.16,
      ),
      titleMedium: textTheme.titleMedium?.copyWith(
        fontWeight: FontWeight.w700,
        height: 1.2,
      ),
      bodyLarge: textTheme.bodyLarge?.copyWith(height: 1.45),
      bodyMedium: textTheme.bodyMedium?.copyWith(height: 1.42),
      bodySmall: textTheme.bodySmall?.copyWith(height: 1.34),
      labelLarge: textTheme.labelLarge?.copyWith(fontWeight: FontWeight.w700),
    ),
    extensions: [AcornStatusColors.fromBrightness(brightness)],
    appBarTheme: AppBarTheme(
      centerTitle: false,
      elevation: 0,
      scrolledUnderElevation: 0,
      backgroundColor: acornScheme.surface,
      foregroundColor: acornScheme.onSurface,
      surfaceTintColor: Colors.transparent,
      titleTextStyle: textTheme.titleLarge?.copyWith(
        color: acornScheme.onSurface,
        fontWeight: FontWeight.w800,
      ),
      toolbarHeight: 64,
    ),
    navigationBarTheme: NavigationBarThemeData(
      height: 78,
      backgroundColor: acornScheme.surfaceContainerLow,
      elevation: 0,
      indicatorColor: acornScheme.primaryContainer,
      indicatorShape: const StadiumBorder(),
      overlayColor: WidgetStatePropertyAll(
        acornScheme.primary.withValues(alpha: 0.08),
      ),
      labelBehavior: NavigationDestinationLabelBehavior.alwaysShow,
      iconTheme: WidgetStateProperty.resolveWith((states) {
        final selected = states.contains(WidgetState.selected);
        return IconThemeData(
          color: selected ? scheme.onPrimaryContainer : scheme.onSurfaceVariant,
          size: selected ? 25 : 23,
        );
      }),
      labelTextStyle: WidgetStateProperty.resolveWith((states) {
        final selected = states.contains(WidgetState.selected);
        return textTheme.labelMedium?.copyWith(
          color: selected ? scheme.onSurface : scheme.onSurfaceVariant,
          fontWeight: selected ? FontWeight.w800 : FontWeight.w600,
        );
      }),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: scheme.surfaceContainerHigh,
      prefixIconColor: scheme.onSurfaceVariant,
      labelStyle: TextStyle(color: scheme.onSurfaceVariant),
      hintStyle: TextStyle(color: scheme.onSurfaceVariant),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(AcornRadius.lg),
        borderSide: BorderSide(color: scheme.outlineVariant),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(AcornRadius.lg),
        borderSide: BorderSide(color: scheme.outlineVariant),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(AcornRadius.lg),
        borderSide: BorderSide(color: scheme.primary, width: 1.6),
      ),
      errorBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(AcornRadius.lg),
        borderSide: BorderSide(color: scheme.error, width: 1.2),
      ),
      focusedErrorBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(AcornRadius.lg),
        borderSide: BorderSide(color: scheme.error, width: 1.4),
      ),
      contentPadding: const EdgeInsets.symmetric(horizontal: 18, vertical: 16),
    ),
    cardTheme: CardThemeData(
      elevation: 0,
      color: scheme.surfaceContainerLow,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AcornRadius.lg),
      ),
      clipBehavior: Clip.antiAlias,
    ),
    listTileTheme: ListTileThemeData(
      iconColor: scheme.onSurfaceVariant,
      textColor: scheme.onSurface,
      selectedColor: scheme.onSecondaryContainer,
      selectedTileColor: scheme.secondaryContainer,
      contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      minLeadingWidth: 40,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AcornRadius.lg),
      ),
      titleTextStyle: textTheme.titleSmall?.copyWith(
        color: scheme.onSurface,
        fontWeight: FontWeight.w800,
      ),
      subtitleTextStyle: textTheme.bodySmall?.copyWith(
        color: scheme.onSurfaceVariant,
      ),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        minimumSize: const Size(64, 48),
        shape: const StadiumBorder(),
        textStyle: textTheme.labelLarge?.copyWith(fontWeight: FontWeight.w700),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        minimumSize: const Size(64, 48),
        foregroundColor: scheme.primary,
        side: BorderSide(color: scheme.outlineVariant),
        shape: const StadiumBorder(),
        textStyle: textTheme.labelLarge?.copyWith(fontWeight: FontWeight.w700),
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(
        minimumSize: const Size(48, 40),
        shape: const StadiumBorder(),
        textStyle: textTheme.labelLarge?.copyWith(fontWeight: FontWeight.w700),
      ),
    ),
    iconButtonTheme: IconButtonThemeData(
      style: IconButton.styleFrom(
        minimumSize: const Size.square(48),
        shape: const CircleBorder(),
        foregroundColor: scheme.onSurfaceVariant,
      ),
    ),
    chipTheme: ChipThemeData(
      backgroundColor: scheme.surfaceContainer,
      selectedColor: scheme.secondaryContainer,
      disabledColor: scheme.onSurface.withValues(alpha: 0.12),
      side: BorderSide(color: scheme.outlineVariant),
      shape: const StadiumBorder(),
      labelStyle: textTheme.labelMedium?.copyWith(color: scheme.onSurface),
      secondaryLabelStyle: textTheme.labelMedium?.copyWith(
        color: scheme.onSecondaryContainer,
      ),
      iconTheme: IconThemeData(color: scheme.onSurfaceVariant, size: 18),
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
    ),
    badgeTheme: BadgeThemeData(
      backgroundColor: scheme.error,
      textColor: scheme.onError,
      textStyle: textTheme.labelSmall?.copyWith(fontWeight: FontWeight.w700),
    ),
    dividerTheme: DividerThemeData(
      color: scheme.outlineVariant,
      thickness: 1,
      space: 1,
    ),
    bottomSheetTheme: BottomSheetThemeData(
      backgroundColor: scheme.surfaceContainerLow,
      surfaceTintColor: Colors.transparent,
      modalBackgroundColor: scheme.surfaceContainerLow,
      modalBarrierColor: scheme.scrim.withValues(alpha: 0.42),
      showDragHandle: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(
          top: Radius.circular(AcornRadius.xl),
        ),
      ),
    ),
    snackBarTheme: SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      backgroundColor: scheme.inverseSurface,
      contentTextStyle: textTheme.bodyMedium?.copyWith(
        color: scheme.onInverseSurface,
      ),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AcornRadius.sm),
      ),
    ),
    progressIndicatorTheme: ProgressIndicatorThemeData(
      color: scheme.primary,
      linearTrackColor: scheme.surfaceContainerHighest,
      circularTrackColor: scheme.surfaceContainerHighest,
    ),
    textSelectionTheme: TextSelectionThemeData(
      cursorColor: scheme.primary,
      selectionColor: scheme.primary.withValues(alpha: 0.28),
      selectionHandleColor: scheme.primary,
    ),
  );
}

FlexSubThemesData _acornSubThemes(Brightness brightness) {
  final dark = brightness == Brightness.dark;
  return FlexSubThemesData(
    interactionEffects: true,
    tintedDisabledControls: true,
    blendOnLevel: dark ? 10 : 6,
    blendOnColors: dark,
    scaffoldBackgroundBaseColor: FlexScaffoldBaseColor.surface,
    useMaterial3Typography: true,
    defaultRadius: AcornRadius.lg,
    buttonMinSize: const Size(64, 48),
    thickBorderWidth: 1.4,
    thinBorderWidth: 1,
    filledButtonRadius: AcornRadius.pill,
    elevatedButtonRadius: AcornRadius.pill,
    outlinedButtonRadius: AcornRadius.pill,
    textButtonRadius: AcornRadius.pill,
    segmentedButtonRadius: AcornRadius.pill,
    inputDecoratorRadius: AcornRadius.lg,
    inputDecoratorIsFilled: true,
    inputDecoratorBackgroundAlpha: dark ? 40 : 26,
    inputDecoratorBorderType: FlexInputBorderType.outline,
    inputDecoratorFocusedBorderWidth: 1.4,
    inputDecoratorUnfocusedHasBorder: true,
    inputDecoratorBorderSchemeColor: SchemeColor.outlineVariant,
    inputDecoratorPrefixIconSchemeColor: SchemeColor.onSurfaceVariant,
    inputDecoratorSuffixIconSchemeColor: SchemeColor.onSurfaceVariant,
    inputDecoratorContentPadding: const EdgeInsets.symmetric(
      horizontal: 18,
      vertical: 16,
    ),
    listTileIconSchemeColor: SchemeColor.onSurfaceVariant,
    listTileContentPadding: const EdgeInsets.symmetric(
      horizontal: 16,
      vertical: 8,
    ),
    cardRadius: AcornRadius.lg,
    cardElevation: 1,
    chipRadius: AcornRadius.pill,
    chipIconSize: 18,
    popupMenuRadius: AcornRadius.md,
    popupMenuElevation: 3,
    menuRadius: AcornRadius.md,
    menuElevation: 3,
    searchBarRadius: AcornRadius.lg,
    searchViewRadius: AcornRadius.lg,
    searchUseGlobalShape: true,
    dialogRadius: AcornRadius.xl,
    bottomSheetRadius: AcornRadius.xl,
    bottomSheetElevation: 0,
    bottomSheetModalElevation: 0,
    appBarBackgroundSchemeColor: SchemeColor.surface,
    appBarForegroundSchemeColor: SchemeColor.onSurface,
    appBarScrolledUnderElevation: 0,
    navigationBarHeight: 78,
    navigationBarBackgroundSchemeColor: SchemeColor.surfaceContainerLow,
    navigationBarIndicatorSchemeColor: SchemeColor.primaryContainer,
    navigationBarIndicatorRadius: AcornRadius.pill,
    navigationBarSelectedIconSchemeColor: SchemeColor.onPrimaryContainer,
    navigationBarSelectedLabelSchemeColor: SchemeColor.onSurface,
    navigationBarUnselectedIconSchemeColor: SchemeColor.onSurfaceVariant,
    navigationBarUnselectedLabelSchemeColor: SchemeColor.onSurfaceVariant,
    navigationBarMutedUnselectedIcon: false,
    navigationBarMutedUnselectedLabel: false,
    navigationBarLabelBehavior: NavigationDestinationLabelBehavior.alwaysShow,
    snackBarRadius: AcornRadius.sm,
    tooltipRadius: AcornRadius.sm,
    progressIndicatorLinearMinHeight: 4,
    progressIndicatorLinearRadius: AcornRadius.pill,
  );
}

ColorScheme _acornColorScheme(Brightness brightness) {
  final base = ColorScheme.fromSeed(
    seedColor: const Color(0xFF4F6F52),
    brightness: brightness,
  );
  if (brightness == Brightness.dark) {
    return base.copyWith(
      primary: const Color(0xFFA9D8A8),
      onPrimary: const Color(0xFF14351A),
      primaryContainer: const Color(0xFF284C2D),
      onPrimaryContainer: const Color(0xFFC4F4C2),
      secondary: const Color(0xFFB8CCB5),
      onSecondary: const Color(0xFF243426),
      secondaryContainer: const Color(0xFF3A4B3B),
      onSecondaryContainer: const Color(0xFFD4E8D0),
      tertiary: const Color(0xFFD9C779),
      onTertiary: const Color(0xFF393008),
      tertiaryContainer: const Color(0xFF514717),
      onTertiaryContainer: const Color(0xFFF6E493),
      error: const Color(0xFFFFB4AB),
      onError: const Color(0xFF690005),
      errorContainer: const Color(0xFF93000A),
      onErrorContainer: const Color(0xFFFFDAD6),
      surface: const Color(0xFF111410),
      onSurface: const Color(0xFFE4E4DB),
      surfaceContainerLowest: const Color(0xFF0B0F0C),
      surfaceContainerLow: const Color(0xFF191D18),
      surfaceContainer: const Color(0xFF1D211C),
      surfaceContainerHigh: const Color(0xFF282C26),
      surfaceContainerHighest: const Color(0xFF333831),
      onSurfaceVariant: const Color(0xFFC2C8BB),
      outline: const Color(0xFF8C9386),
      outlineVariant: const Color(0xFF42493F),
      inverseSurface: const Color(0xFFE4E4DB),
      onInverseSurface: const Color(0xFF2E312D),
      inversePrimary: const Color(0xFF3F6A41),
      surfaceTint: const Color(0xFFA9D8A8),
    );
  }

  return base.copyWith(
    primary: const Color(0xFF3F6A41),
    onPrimary: const Color(0xFFF7FFF1),
    primaryContainer: const Color(0xFFC4F4C2),
    onPrimaryContainer: const Color(0xFF09210D),
    secondary: const Color(0xFF58634F),
    onSecondary: const Color(0xFFFCFFF5),
    secondaryContainer: const Color(0xFFDCE8CF),
    onSecondaryContainer: const Color(0xFF161F14),
    tertiary: const Color(0xFF6A5E21),
    onTertiary: const Color(0xFFFFFBEB),
    tertiaryContainer: const Color(0xFFF6E493),
    onTertiaryContainer: const Color(0xFF211B00),
    error: const Color(0xFFBA1A1A),
    onError: const Color(0xFFFFFBFF),
    errorContainer: const Color(0xFFFFDAD6),
    onErrorContainer: const Color(0xFF410002),
    surface: const Color(0xFFFAFBF4),
    onSurface: const Color(0xFF1B1C18),
    surfaceContainerLowest: const Color(0xFFFFFFF8),
    surfaceContainerLow: const Color(0xFFF4F5ED),
    surfaceContainer: const Color(0xFFEEEFE7),
    surfaceContainerHigh: const Color(0xFFE8E9E1),
    surfaceContainerHighest: const Color(0xFFE2E4DC),
    onSurfaceVariant: const Color(0xFF454C42),
    outline: const Color(0xFF73796D),
    outlineVariant: const Color(0xFFC3C8BC),
    inverseSurface: const Color(0xFF30312D),
    onInverseSurface: const Color(0xFFF2F1EA),
    inversePrimary: const Color(0xFFA9D8A8),
    surfaceTint: const Color(0xFF3F6A41),
  );
}

abstract final class AcornSpacing {
  static const xs = 4.0;
  static const sm = 8.0;
  static const md = 12.0;
  static const lg = 16.0;
  static const xl = 24.0;
  static const xxl = 32.0;
}

abstract final class AcornRadius {
  static const sm = 8.0;
  static const md = 12.0;
  static const lg = 16.0;
  static const xl = 24.0;
  static const pill = 999.0;
}

enum AcornStatusTone { success, warning, info, neutral, error }

@immutable
final class AcornStatusColor {
  const AcornStatusColor({
    required this.color,
    required this.onColor,
    required this.container,
    required this.onContainer,
  });

  final Color color;
  final Color onColor;
  final Color container;
  final Color onContainer;

  static AcornStatusColor lerp(
    AcornStatusColor a,
    AcornStatusColor b,
    double t,
  ) {
    return AcornStatusColor(
      color: Color.lerp(a.color, b.color, t)!,
      onColor: Color.lerp(a.onColor, b.onColor, t)!,
      container: Color.lerp(a.container, b.container, t)!,
      onContainer: Color.lerp(a.onContainer, b.onContainer, t)!,
    );
  }
}

@immutable
final class AcornStatusColors extends ThemeExtension<AcornStatusColors> {
  const AcornStatusColors({
    required this.success,
    required this.warning,
    required this.info,
    required this.neutral,
    required this.error,
  });

  final AcornStatusColor success;
  final AcornStatusColor warning;
  final AcornStatusColor info;
  final AcornStatusColor neutral;
  final AcornStatusColor error;

  factory AcornStatusColors.fromBrightness(Brightness brightness) {
    final dark = brightness == Brightness.dark;
    if (dark) {
      return const AcornStatusColors(
        success: AcornStatusColor(
          color: Color(0xFF9DD67D),
          onColor: Color(0xFF0C3900),
          container: Color(0xFF205107),
          onContainer: Color(0xFFB8F397),
        ),
        warning: AcornStatusColor(
          color: Color(0xFFEAC247),
          onColor: Color(0xFF3E2E00),
          container: Color(0xFF5A4300),
          onContainer: Color(0xFFFFDF85),
        ),
        info: AcornStatusColor(
          color: Color(0xFF9FCBFF),
          onColor: Color(0xFF003258),
          container: Color(0xFF00497D),
          onContainer: Color(0xFFD0E4FF),
        ),
        neutral: AcornStatusColor(
          color: Color(0xFFC7C8BE),
          onColor: Color(0xFF30312C),
          container: Color(0xFF474840),
          onContainer: Color(0xFFE3E4D9),
        ),
        error: AcornStatusColor(
          color: Color(0xFFFFB4AB),
          onColor: Color(0xFF690005),
          container: Color(0xFF93000A),
          onContainer: Color(0xFFFFDAD6),
        ),
      );
    }

    return const AcornStatusColors(
      success: AcornStatusColor(
        color: Color(0xFF386A20),
        onColor: Color(0xFFF7FFF0),
        container: Color(0xFFB8F397),
        onContainer: Color(0xFF042100),
      ),
      warning: AcornStatusColor(
        color: Color(0xFF765B00),
        onColor: Color(0xFFFFFBF2),
        container: Color(0xFFFFDF85),
        onContainer: Color(0xFF241A00),
      ),
      info: AcornStatusColor(
        color: Color(0xFF0061A4),
        onColor: Color(0xFFF6FAFF),
        container: Color(0xFFD0E4FF),
        onContainer: Color(0xFF001D36),
      ),
      neutral: AcornStatusColor(
        color: Color(0xFF5F6159),
        onColor: Color(0xFFFCFDF2),
        container: Color(0xFFE3E4D9),
        onContainer: Color(0xFF1C1C18),
      ),
      error: AcornStatusColor(
        color: Color(0xFFBA1A1A),
        onColor: Color(0xFFFFFBFF),
        container: Color(0xFFFFDAD6),
        onContainer: Color(0xFF410002),
      ),
    );
  }

  static AcornStatusColors of(BuildContext context) {
    return Theme.of(context).extension<AcornStatusColors>() ??
        AcornStatusColors.fromBrightness(Theme.of(context).brightness);
  }

  AcornStatusColor tone(AcornStatusTone tone) {
    return switch (tone) {
      AcornStatusTone.success => success,
      AcornStatusTone.warning => warning,
      AcornStatusTone.info => info,
      AcornStatusTone.neutral => neutral,
      AcornStatusTone.error => error,
    };
  }

  @override
  AcornStatusColors copyWith({
    AcornStatusColor? success,
    AcornStatusColor? warning,
    AcornStatusColor? info,
    AcornStatusColor? neutral,
    AcornStatusColor? error,
  }) {
    return AcornStatusColors(
      success: success ?? this.success,
      warning: warning ?? this.warning,
      info: info ?? this.info,
      neutral: neutral ?? this.neutral,
      error: error ?? this.error,
    );
  }

  @override
  AcornStatusColors lerp(
    covariant ThemeExtension<AcornStatusColors>? other,
    double t,
  ) {
    if (other is! AcornStatusColors) {
      return this;
    }
    return AcornStatusColors(
      success: AcornStatusColor.lerp(success, other.success, t),
      warning: AcornStatusColor.lerp(warning, other.warning, t),
      info: AcornStatusColor.lerp(info, other.info, t),
      neutral: AcornStatusColor.lerp(neutral, other.neutral, t),
      error: AcornStatusColor.lerp(error, other.error, t),
    );
  }
}
