// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { useT } from "@/app/i18n/react";

const UpgradeOnboardingModal_v0_13_1_Content = () => {
    const tt = useT();
    return (
        <div className="flex flex-col items-start gap-6 w-full mb-4 unselectable">
            <div className="text-secondary leading-relaxed">
                <p className="mb-0">
                    {tt(
                        "Wave v0.13.1 focuses on Windows platform improvements, Wave AI visual updates, and enhanced terminal navigation."
                    )}
                </p>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-brands fa-windows"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">
                        {tt("Windows Platform Enhancements")}
                    </div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("Integrated Window Layout")}</strong> -{" "}
                                {tt("Cleaner interface with controls integrated into the tab-bar header")}
                            </li>
                            <li>
                                <strong>{tt("Git Bash Auto-Detection")}</strong> -{" "}
                                {tt("Automatically detects Git Bash installations")}
                            </li>
                            <li>
                                <strong>{tt("SSH Agent Fallback")}</strong> -{" "}
                                {tt("Improved SSH agent support on Windows")}
                            </li>
                            <li>
                                <strong>{tt("Updated Focus Keybinding")}</strong> -{" "}
                                {tt("Wave AI focus key changed to Alt:0 on Windows")}
                            </li>
                        </ul>
                    </div>
                </div>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-sparkles"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Wave AI Updates")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("Refreshed Visual Design")}</strong> -{" "}
                                {tt("Complete UI refresh with transparency support for custom backgrounds")}
                            </li>
                            <li>
                                <strong>{tt("BYOK Without Telemetry")}</strong> -{" "}
                                {tt(
                                    "Wave AI now works with bring-your-own-key and local models without requiring telemetry"
                                )}
                            </li>
                        </ul>
                    </div>
                </div>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-terminal"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Terminal Improvements")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("New Scrolling Keybindings")}</strong> -{" "}
                                {tt(
                                    "Added Shift+Home, Shift+End, Shift+PageUp, and Shift+PageDown for better navigation"
                                )}
                            </li>
                        </ul>
                    </div>
                </div>
            </div>
        </div>
    );
};

UpgradeOnboardingModal_v0_13_1_Content.displayName = "UpgradeOnboardingModal_v0_13_1_Content";

export { UpgradeOnboardingModal_v0_13_1_Content };