// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import type { MessageBoxOptions, SaveDialogOptions } from "electron";
import { t, type Locale } from "./index";

export function menuLabel(key: string, locale: Locale): string {
    return t(key, undefined, locale);
}

export function makeQuitConfirmDialog(locale: Locale): MessageBoxOptions {
    return {
        type: "question",
        buttons: [t("Cancel", undefined, locale), t("Quit", undefined, locale)],
        title: t("Confirm Quit", undefined, locale),
        message: t("Are you sure you want to quit Wave Terminal?", undefined, locale),
        defaultId: 0,
        cancelId: 0,
    };
}

export function makeCloseTabDialog(locale: Locale): MessageBoxOptions {
    return {
        type: "question",
        defaultId: 1,
        cancelId: 0,
        buttons: [t("Cancel", undefined, locale), t("Close Tab", undefined, locale)],
        title: t("Confirm", undefined, locale),
        message: t("Are you sure you want to close this tab?", undefined, locale),
    };
}

export function makeCloseWindowDialog(locale: Locale): MessageBoxOptions {
    return {
        type: "question",
        buttons: [t("Cancel", undefined, locale), t("Close Window", undefined, locale)],
        title: t("Confirm", undefined, locale),
        message: t(
            "Window has unsaved tabs, closing window will delete existing tabs.\n\nContinue?",
            undefined,
            locale
        ),
    };
}

export function makeDeleteWorkspaceDialog(locale: Locale): MessageBoxOptions {
    return {
        type: "question",
        buttons: [t("Cancel", undefined, locale), t("Delete Workspace", undefined, locale)],
        title: t("Confirm", undefined, locale),
        message: t("Deleting workspace will also delete its contents.\n\nContinue?", undefined, locale),
    };
}

export function makeNoUpdatesDialog(locale: Locale): MessageBoxOptions {
    return {
        type: "info",
        message: t("There are currently no updates available.", undefined, locale),
    };
}

export function makeUpdateReadyDialog(
    locale: Locale,
    releaseName: string | null,
    releaseNotes: string | null,
    platform: string
): MessageBoxOptions {
    return {
        type: "info",
        buttons: [t("Restart", undefined, locale), t("Later", undefined, locale)],
        title: t("Application Update", undefined, locale),
        message: platform === "win32" ? releaseNotes : releaseName,
        detail: t("A new version has been downloaded. Restart the application to apply the updates.", undefined, locale),
    };
}

export function makeArm64WarningDialog(locale: Locale): MessageBoxOptions {
    return {
        type: "warning",
        buttons: [t("Dismiss", undefined, locale), t("Learn More", undefined, locale)],
        title: t("Wave has detected a performance issue", undefined, locale),
        message: t(
            "Wave is running in ARM64 translation mode which may impact performance.\n\nRecommendation: Download the native ARM64 version from our website for optimal performance.",
            undefined,
            locale
        ),
    };
}

export function makeUpdateNotificationBody(locale: Locale): string {
    return t("A new version of Wave Terminal is ready to install.", undefined, locale);
}

export function makeSaveScrollbackDialog(locale: Locale, defaultPath: string): SaveDialogOptions {
    return {
        title: t("Save Scrollback", undefined, locale),
        defaultPath,
        buttonLabel: t("Save", undefined, locale),
        filters: [{ name: t("Text Files", undefined, locale), extensions: ["txt", "log"] }],
    };
}

export function makeSaveImageDialog(locale: Locale, defaultPath: string): SaveDialogOptions {
    return {
        title: t("Save Image", undefined, locale),
        defaultPath,
        buttonLabel: t("Save", undefined, locale),
        filters: [
            {
                name: t("Images", undefined, locale),
                extensions: ["png", "jpg", "jpeg", "gif", "webp", "bmp", "tiff", "heic"],
            },
        ],
    };
}