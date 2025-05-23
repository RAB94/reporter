/*
   Copyright 2016 Vastech SA (PTY) LTD

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package report

// Default Grid-based template (No changes needed here)
const defaultTemplate = `
%use square brackets as golang text templating delimiters
\documentclass{article}
\usepackage{graphicx}
\usepackage[margin=1in]{geometry}
\usepackage{amsmath} % For text formatting options if needed
\usepackage{fancyhdr} % For headers/footers
\pagestyle{fancy}

% Footer configuration
\fancyfoot[L]{[[ EscapeLaTeX .Title ]]} % Escape title
\fancyfoot[C]{Generated by Grafana Reporter}
\fancyfoot[R]{Page \thepage}

% Header configuration (Example - might need image or different text)
% \fancyhead[C]{[[ EscapeLaTeX .Title ]]} % Example Header
\renewcommand{\headrulewidth}{0pt} % Remove header rule if header is empty

\graphicspath{ {[[.ImgDir]]/} } % Use ImgDir variable - Single braces

\begin{document}
% Simple \title, \date, \author for maketitle
\title{[[ EscapeLaTeX .Title ]]}
\date{From: [[.FromFormatted]] To: [[.ToFormatted]]} % Uses explicit fields
\author{Grafana Reporter} % Added Author

\maketitle % Generate title block

% Display VariableValues and Description below the main title if they exist
\begin{center}
[[if .VariableValues]] \large [[ EscapeLaTeX .VariableValues ]] \par \vspace{2mm} [[end]]
[[if .Description]] \small [[ EscapeLaTeX .Description ]] \par \vspace{4mm} [[end]]
\end{center}

\thispagestyle{fancy} % Apply fancy style to first page too

\begin{center}
% Use explicit Panels field
[[range .Panels]]
    % Check panel type using helper function if needed, or directly
    [[if (eq .Type "singlestat")]] % Example direct check
        \begin{minipage}{0.3\textwidth} % Adjust width as needed
            \includegraphics[width=\textwidth]{[[ PanelImagePath .Id ]]} % Use PanelImagePath helper
            % Use simple text formatting for title instead of caption
            \par { \small [[ EscapeLaTeX .Title ]] } \par
        \end{minipage}
    [[else]] % Handle other panel types (graph, table etc.)
        \par % Ensure block starts on new line
        \vspace{0.5cm}
        \includegraphics[width=0.9\textwidth]{[[ PanelImagePath .Id ]]} % Use PanelImagePath helper
        % Use simple text formatting for title instead of caption
        \par { \small [[ EscapeLaTeX .Title ]] } \par
        \vspace{0.5cm}
    [[end]]
[[end]] % End range Panels
\end{center}

\end{document}
`

// Row-based template - **MODIFIED to remove \caption* **
const rowBasedTemplate = `
%use square brackets as golang text templating delimiters
\documentclass[landscape]{article}
\usepackage[utf8]{inputenc}
\usepackage{graphicx}
% Adjust paper size and margins for landscape
\usepackage[paperwidth=11in, paperheight=8.5in, margin=0.5in]{geometry}
\usepackage{amsmath} % For text formatting options if needed
\usepackage{fancyhdr} % For headers/footers
\pagestyle{fancy}

% Footer configuration
\fancyfoot[L]{[[ EscapeLaTeX .Title ]]} % Escape title
\fancyfoot[C]{Splitpoint Solutions} % Use your desired fixed text
\fancyfoot[R]{Page \thepage}

% Set header height appropriately to fit the image
\setlength\headheight{80pt} % Adjust based on image height and desired spacing

% Header configuration - adjust width for landscape mode
% Ensure the path to the header image is correct and accessible by the LaTeX compiler
\lhead{\includegraphics[width=0.9\paperwidth,height=2cm,keepaspectratio]{/home/sps/reporter-images-DO-NOT-DELETE/REPORT-HEADER-05.png}} % Check path carefully!

% Tell LaTeX where to find images (relative to the .tex file)
\graphicspath{ {[[.ImgDir]]/} }

\begin{document}
% --- Simplified Title Block ---
\title{[[ EscapeLaTeX .Title ]]}
\date{Time Range: [[.FromFormatted]] to [[.ToFormatted]]} % Use explicit fields
\author{Generated Report}
\maketitle
% --- End Title Block ---

\thispagestyle{fancy} % Apply fancy style to first page too

% --- Optional: Display Variables and Description Below Title ---
\begin{center} % Center the variables and description
 [[if .VariableValues]] % Check if VariableValues exist
    \large [[ EscapeLaTeX .VariableValues ]] % Display escaped variables
    \par \vspace{2mm} % Add a paragraph break & space
 [[end]]
 [[if .Description]] % Check if Description exists
    \small [[ EscapeLaTeX .Description ]] % Display escaped description
    \par \vspace{4mm} % Add a paragraph break & space
 [[end]]
\end{center}
% --- End Optional Variables/Description ---


% Brief explanation of the report
\begin{center}
\large{The following pages contain sections from the Grafana dashboard}
\end{center}

% Display dashboard rows - one per page - in order
[[range .Rows]]
\newpage % Start each row on a new page
\thispagestyle{fancy} % Apply fancy style to subsequent pages

% --- Row Header ---
\begin{center}
\Large\textbf{[[ EscapeLaTeX .Title ]]} % Display row title (from GrafanaRow)
\vspace{0.5cm}
\end{center}
% --- End Row Header ---

% --- Display Panels WITHIN this Row ---
\begin{center} % Center the panel images
  % Loop through the ContentPanels associated with the current row
  [[range .ContentPanels]]
    % Basic layout: display each panel image centered on its own line
    \par % Ensure panels are below each other
    \includegraphics[width=0.9\textwidth, keepaspectratio]{[[ PanelImagePath .Id ]]} % Include panel image
    % *** CHANGE: Replace \caption* with simple text formatting ***
    \par % Ensure title starts on new line below image
    { \small [[ EscapeLaTeX .Title ]] } % Display title as small text, centered by parent environment
    \par % Ensure space after title
    \vspace{0.5cm} % Add space between panels
  [[end]] % End range .ContentPanels
\end{center}
% --- End Display Panels ---

[[end]] % End range .Rows

\end{document}
`
