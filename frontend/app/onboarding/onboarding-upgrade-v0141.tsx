// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { useT } from "@/app/i18n/react";

const UpgradeOnboardingModal_v0_14_1_Content = () => {
    const tt = useT();
    return (
        <div className="flex flex-col items-start w-full mb-2 unselectable">
            <div className="text-secondary leading-relaxed mb-4">
                <p className="mb-0">
                    {tt(
                        "Wave v0.14.1 fixes several high-impact terminal bugs and adds new config options for focus, cursor style, and block navigation."
                    )}
                </p>
            </div>

            <div className="flex w-full items-start gap-4 mb-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-terminal"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Terminal Fixes")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("Claude Code Scroll Fix")}</strong> -{" "}
                                {tt("Fixed unexpected terminal scroll jumps")}
                            </li>
                            <li>
                                <strong>{tt("IME Fix")}</strong> -{" "}
                                {tt("Fixed Korean/CJK input losing or sticking characters")}
                            </li>
                            <li>
                                <strong>{tt("Scroll Position on Resize")}</strong> -{" "}
                                {tt("Terminal stays at bottom across resizes")}
                            </li>
                            <li>
                                <strong>{tt("Terminal Scrollback Save")}</strong> -{" "}
                                {tt("New context menu item and")} <code>wsh</code>{" "}
                                {tt("command to save scrollback to a file")}
                            </li>
                        </ul>
                    </div>
                </div>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-sliders"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("New Config Options")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("Focus Follows Cursor")}</strong> - {tt("New")}{" "}
                                <code>app:focusfollowscursor</code> {tt("setting (off/on/term)")}
                            </li>
                            <li>
                                <strong>{tt("Terminal Cursor Style & Blink")}</strong> -{" "}
                                {tt("Configure cursor shape and blink per-block")}
                            </li>
                            <li>
                                <strong>{tt("Vim-Style Block Navigation")}</strong> -{" "}
                                {tt("Ctrl+Shift+H/J/K/L to navigate blocks")}
                            </li>
                            <li>
                                <strong>{tt("New AI Providers")}</strong> -{" "}
                                {tt("Added Groq and NanoGPT as built-in presets")}
                            </li>
                        </ul>
                    </div>
                </div>
            </div>
        </div>
    );
};

UpgradeOnboardingModal_v0_14_1_Content.displayName = "UpgradeOnboardingModal_v0_14_1_Content";

export { UpgradeOnboardingModal_v0_14_1_Content };