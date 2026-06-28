// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { useT } from "@/app/i18n/react";

const UpgradeOnboardingModal_v0_14_4_Content = () => {
    const tt = useT();
    return (
        <div className="flex flex-col items-start gap-6 w-full mb-4 unselectable">
            <div className="text-secondary leading-relaxed">
                <p className="mb-0">
                    {tt(
                        "Wave v0.14.4 introduces vertical tabs, upgrades to xterm.js v6, and includes bug fixes and UI improvements."
                    )}
                </p>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-table-columns"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Vertical Tab Bar")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("New Vertical Tab Bar Option")}</strong> -{" "}
                                {tt(
                                    "Tabs can now be displayed vertically along the side of the window for more horizontal space. Toggle between horizontal and vertical layouts in settings."
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
                                <strong>{tt("xterm.js v6.0.0 Upgrade")}</strong> -{" "}
                                {tt(
                                    "Improved terminal compatibility and rendering, resolving quirks with tools like Claude Code"
                                )}
                            </li>
                        </ul>
                    </div>
                </div>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-wrench"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Other Changes")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("macOS First Click")}</strong> -{" "}
                                {tt("First click now focuses the clicked widget")}
                            </li>
                            <li>
                                <strong>
                                    <code>backgrounds.json</code>
                                </strong>{" "}
                                - {tt("Renamed")} <code>presets/bg.json</code> {tt("to")} <code>backgrounds.json</code>
                            </li>
                            <li>
                                <strong>{tt("Config Errors Moved")}</strong> -{" "}
                                {tt("Config errors to the WaveConfig view for less clutter")}
                            </li>
                            <li>{tt("WaveConfig now warns on Unsaved Changes")}</li>
                            <li>{tt("Preview streaming fixes for images/videos")}</li>
                            <li>{tt("Deprecated legacy AI widget has been removed")}</li>
                            <li>[bugfix] {tt("Fixed focus bug for newly created blocks")}</li>
                        </ul>
                    </div>
                </div>
            </div>
        </div>
    );
};

UpgradeOnboardingModal_v0_14_4_Content.displayName = "UpgradeOnboardingModal_v0_14_4_Content";

export { UpgradeOnboardingModal_v0_14_4_Content };