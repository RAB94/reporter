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

const defaultTemplate = `
%use square brackets as golang text templating delimiters
\documentclass{article}
\usepackage{graphicx}
\usepackage[margin=1in]{geometry}

\graphicspath{ {images/} }
\begin{document}
\title{[[.Title]] [[if .VariableValues]] \\ \large [[.VariableValues]] [[end]] [[if .Description]] \\ \small [[.Description]] [[end]]}
\date{[[.FromFormatted]]\\to\\[[.ToFormatted]]}
\maketitle
\begin{center}
[[range .Panels]][[if .IsSingleStat]]\begin{minipage}{0.3\textwidth}
\includegraphics[width=\textwidth]{image[[.Id]]}
\end{minipage}
[[else]]\par
\vspace{0.5cm}
\includegraphics[width=\textwidth]{image[[.Id]]}
\par
\vspace{0.5cm}
[[end]][[end]]

\end{center}
\end{document}
`

// Row-based template with landscape orientation
const rowBasedTemplate = `
%use square brackets as golang text templating delimiters
\documentclass[landscape]{article}
\usepackage[utf8]{inputenc}
\usepackage{graphicx}
\usepackage[paperwidth=11in,paperheight=8.5in,margin=0.5in]{geometry}
\usepackage{amsmath}
\usepackage{fancyhdr}
\pagestyle{fancy}

% Footer configuration
\fancyfoot[L]{[[.Title]]}
\fancyfoot[C]{Splitpoint Solutions}
\fancyfoot[R]{Page \thepage}

% Set header height appropriately
\setlength\headheight{80pt}

% Header configuration - adjust width for landscape mode
\lhead{\includegraphics[width=0.9\paperwidth,height=2cm]{/home/sps/reporter-images-DO-NOT-DELETE/REPORT-HEADER-05.png}}

\graphicspath{ {images/} }
\begin{document}
\title{[[.Title]] [[if .VariableValues]] \\ \large [[.VariableValues]] [[end]] [[if .Description]] \\ \small [[.Description]] [[end]]}
\date{[[.FromFormatted]]\\to\\[[.ToFormatted]]}
\maketitle
\thispagestyle{fancy}

% Display dashboard rows one per page
[[range .GetRows]]
\newpage
\thispagestyle{fancy}
\vspace*{0.5cm}
\begin{center}
\includegraphics[width=0.95\paperwidth,height=0.7\paperheight,keepaspectratio]{row[[.Id]]}
\end{center}
[[end]]

\end{document}
`
