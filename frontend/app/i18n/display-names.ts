// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { t, type Locale } from "./index";

/** System preset IDs from pkg/wconfig/defaultconfig/backgrounds.json */
const BACKGROUND_PRESET_NAME_KEYS: Record<string, string> = {
    "bg@rainbow": "Rainbow",
    "bg@green": "Green",
    "bg@blue": "Blue",
    "bg@red": "Red",
    "bg@ocean-depths": "Ocean Depths",
    "bg@aqua-horizon": "Aqua Horizon",
    "bg@sunset": "Sunset",
    "bg@enchantedforest": "Enchanted Forest",
    "bg@twilight-mist": "Twilight Mist",
    "bg@duskhorizon": "Dusk Horizon",
    "bg@tropical-radiance": "Tropical Radiance",
    "bg@twilight-ember": "Twilight Ember",
    "bg@cosmic-tide": "Cosmic Tide",
};

/** System preset IDs from pkg/wconfig/defaultconfig/termthemes.json */
const TERM_THEME_PRESET_NAME_KEYS: Record<string, string> = {
    "default-dark": "Default Dark",
    onedarkpro: "One Dark Pro",
    dracula: "Dracula",
    monokai: "Monokai",
    campbell: "Campbell",
    warmyellow: "Warm Yellow",
    rosepine: "Rose Pine",
};

/** System preset IDs from pkg/wconfig/defaultconfig/widgets.json */
const WIDGET_PRESET_LABEL_KEYS: Record<string, string> = {
    "defwidget@terminal": "terminal",
    "defwidget@files": "files",
    "defwidget@web": "web",
    "defwidget@sysinfo": "sysinfo",
    "defwidget@processviewer": "processes",
};

/** waveai@* from pkg/wconfig/defaultconfig/waveai.json and waveaibuilder@* built-ins */
const AI_MODE_NAME_KEYS: Record<string, string> = {
    "waveai@quick": "Quick",
    "waveai@balanced": "Balanced",
    "waveai@deep": "Deep",
    "waveaibuilder@default": "Builder Default",
    "waveaibuilder@deep": "Builder Deep",
};

const AI_MODE_DESCRIPTION_KEYS: Record<string, string> = {
    "waveai@quick": "Fastest responses (gpt-5-mini)",
    "waveai@balanced": "Good mix of speed and accuracy\n(gpt-5.1 with minimal thinking)",
    "waveai@deep": "Slower but most capable\n(gpt-5.1 with full reasoning)",
    "waveaibuilder@default": "Good mix of speed and accuracy\n(gpt-5.4 with minimal thinking)",
    "waveaibuilder@deep": "Slower but most capable\n(gpt-5.4 with full reasoning)",
};

export function isSystemBackgroundPreset(id: string): boolean {
    return id in BACKGROUND_PRESET_NAME_KEYS;
}

export function isSystemTermThemePreset(id: string): boolean {
    return id in TERM_THEME_PRESET_NAME_KEYS;
}

export function isSystemWidgetPreset(id: string): boolean {
    return id.startsWith("defwidget@") && id in WIDGET_PRESET_LABEL_KEYS;
}

export function isSystemCloudAIMode(id: string): boolean {
    return id.startsWith("waveai@");
}

export function isSystemBuilderAIMode(id: string): boolean {
    return id.startsWith("waveaibuilder@");
}

export function isSystemAIMode(id: string): boolean {
    return id in AI_MODE_NAME_KEYS;
}

export function localizeBackgroundName(id: string, fallback: string, locale: Locale): string {
    const key = BACKGROUND_PRESET_NAME_KEYS[id];
    if (key) {
        return t(key, undefined, locale);
    }
    return fallback;
}

export function localizeTermThemeName(id: string, fallback: string, locale: Locale): string {
    const key = TERM_THEME_PRESET_NAME_KEYS[id];
    if (key) {
        return t(key, undefined, locale);
    }
    return fallback;
}

export function localizeWidgetLabel(id: string, fallback: string, locale: Locale): string {
    const key = WIDGET_PRESET_LABEL_KEYS[id];
    if (key) {
        return t(key, undefined, locale);
    }
    return fallback;
}

export function localizeWidgetDescription(
    id: string,
    fallback: string | undefined,
    locale: Locale
): string | undefined {
    if (!fallback || !isSystemWidgetPreset(id)) {
        return fallback;
    }
    return fallback;
}

export function localizeAIModeDisplayName(
    modeId: string | undefined,
    fallback: string | undefined,
    locale: Locale
): string {
    if (modeId) {
        const key = AI_MODE_NAME_KEYS[modeId];
        if (key) {
            return t(key, undefined, locale);
        }
    }
    return fallback ?? "";
}

export function localizeAIModeDisplayDescription(
    modeId: string | undefined,
    fallback: string | undefined | null,
    locale: Locale
): string | null {
    if (modeId) {
        const key = AI_MODE_DESCRIPTION_KEYS[modeId];
        if (key) {
            return t(key, undefined, locale);
        }
    }
    return fallback ?? null;
}

export function widgetMatchesSearch(
    id: string,
    widget: WidgetConfigType,
    searchTerm: string,
    locale: Locale
): boolean {
    if (!searchTerm) {
        return true;
    }
    const lower = searchTerm.toLowerCase();
    const localizedLabel = localizeWidgetLabel(id, widget.label ?? "", locale);
    if (localizedLabel.toLowerCase().includes(lower)) {
        return true;
    }
    const localizedDesc = localizeWidgetDescription(id, widget.description, locale);
    if (localizedDesc?.toLowerCase().includes(lower)) {
        return true;
    }
    if (widget.label?.toLowerCase().includes(lower)) {
        return true;
    }
    if (widget.description?.toLowerCase().includes(lower)) {
        return true;
    }
    return false;
}

export function sortWidgetEntriesByDisplayOrder(
    entries: { id: string; widget: WidgetConfigType }[]
): { id: string; widget: WidgetConfigType }[] {
    return [...entries].sort(
        (a, b) => (a.widget["display:order"] ?? 0) - (b.widget["display:order"] ?? 0)
    );
}